const { app, BrowserWindow, ipcMain, session, globalShortcut, Menu } = require('electron');
const path = require('path');
const { spawn } = require('child_process');

let mainWin;
let apiKey = null;
let rustProcess = null;
let rustStdin = null;

// Создаем HTML для экрана загрузки
const loadingHTML = `
<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>Загрузка...</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
      margin: 0;
      padding: 0;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      height: 100vh;
      background: #0f172a;
      color: white;
      overflow: hidden;
    }
    
    h2 {
      margin-bottom: 30px;
      font-weight: 500;
    }
    
    .spinner {
      width: 50px;
      height: 50px;
      border: 5px solid rgba(255, 255, 255, 0.2);
      border-radius: 50%;
      border-top-color: white;
      animation: spin 1s ease-in-out infinite;
    }
    
    @keyframes spin {
      to { transform: rotate(360deg); }
    }
    
    .message {
      margin-top: 20px;
      opacity: 0.8;
    }
  </style>
</head>
<body>
  <h2>Загрузка капчи</h2>
  <div class="spinner"></div>
  <div class="message">Пожалуйста, подождите...</div>
</body>
</html>
`;

app.on('ready', () => {
  session.defaultSession.webRequest.onHeadersReceived((details, callback) => {
    callback({
      responseHeaders: {
        ...details.responseHeaders,
        'content-security-policy': [''],
        'content-security-policy-report-only': [''],
        'x-frame-options': [''],
        'x-content-type-options': [''],
        'access-control-allow-origin': ['*']
      }
    });
  });

  globalShortcut.register('Control+Shift+I', () => {
    if (mainWin && !mainWin.isDestroyed()) {
      mainWin.webContents.toggleDevTools();
    }
  });
  
  globalShortcut.register('F12', () => {
    if (mainWin && !mainWin.isDestroyed()) {
      mainWin.webContents.toggleDevTools();
    }
  });
});

app.on('will-quit', () => {
  globalShortcut.unregisterAll();
});

function createWindow(htmlFile) {
  mainWin = new BrowserWindow({
    width: 800,
    height: 600,
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      devTools: true
    }
  });

  const contextMenu = Menu.buildFromTemplate([
    { label: 'Инспектировать элемент', click: () => mainWin.webContents.inspectElement(0, 0) },
    { label: 'Открыть инструменты разработчика', click: () => mainWin.webContents.toggleDevTools() }
  ]);

  mainWin.webContents.on('context-menu', (e, params) => {
    contextMenu.popup();
  });

  mainWin.loadFile(htmlFile);
  mainWin.once('ready-to-show', () => {
    mainWin.show();
    mainWin.webContents.openDevTools({ mode: 'detach' });
  });
}

app.whenReady().then(() => {
  createWindow('auth.html');
});

function getRustPath() {
  return process.platform === 'win32'
    ? path.join(process.resourcesPath, 'captcha_cli.exe')
    : path.join(process.resourcesPath, 'captcha_cli');
}

ipcMain.handle('auth:login', async (_event, apiKey) => {
  console.log("🔐 Получен ключ:", apiKey);
  const rustPath = getRustPath();
  console.log("📂 Using Rust binary at:", rustPath);

  try {
    const rust = spawn(rustPath, ['auth']);
    return new Promise((resolve) => {
      rust.stdin.write(JSON.stringify({ api_key: apiKey }) + '\n');
      rust.stdin.end();

      let output = '';
      rust.stdout.on('data', (data) => {
        output += data.toString();
      });

      rust.stderr.on('data', (data) => {
        console.error("RUST STDERR:", data.toString());
      });

      rust.on('error', (err) => {
        console.error("❌ Spawn error:", err);
        resolve({ ok: false, message: 'Failed to start Rust process' });
      });

      rust.on('close', () => {
        try {
          const response = JSON.parse(output.trim());
          if (response.status === 'ok') {
            global.apiKey = apiKey;
            mainWin.loadFile('menu.html');
            resolve({ ok: true, balance: response.balance });
          } else {
            resolve({ ok: false, message: response.message || 'Authentication failed' });
          }
        } catch (e) {
          console.error("❌ Ошибка парсинга ответа:", e);
          resolve({ ok: false, message: 'Server response error' });
        }
      });
    });
  } catch (err) {
    console.error("❌ Critical error:", err);
    return { ok: false, message: 'Failed to start process' };
  }
});

ipcMain.handle('get:balance', async () => {
  if (!global.apiKey) {
    return { ok: false };
  }

  const rustPath = getRustPath();
  console.log("📂 Using Rust binary at:", rustPath);

  try {
    const rust = spawn(rustPath, ['auth']);
    return new Promise((resolve) => {
      rust.stdin.write(JSON.stringify({ api_key: global.apiKey }) + '\n');
      rust.stdin.end();

      let output = '';
      rust.stdout.on('data', (data) => {
        output += data.toString();
      });

      rust.stderr.on('data', (data) => {
        console.error("RUST STDERR:", data.toString());
      });

      rust.on('error', (err) => {
        console.error("❌ Spawn error:", err);
        resolve({ ok: false });
      });

      rust.on('close', () => {
        try {
          const response = JSON.parse(output.trim());
          if (response.status === 'ok') {
            resolve({ ok: true, balance: response.balance });
          } else {
            resolve({ ok: false });
          }
        } catch (e) {
          console.error("❌ Ошибка парсинга ответа:", e);
          resolve({ ok: false });
        }
      });
    });
  } catch (err) {
    console.error("❌ Critical error:", err);
    return { ok: false };
  }
});

ipcMain.on('menu:solve', () => {
  if (!rustProcess) {
    startRustSolver();
  } else {
    requestNewTask();
  }
});

function startRustSolver() {
  if (!global.apiKey) {
    console.error("❌ API ключ не найден!");
    return;
  }

  console.log("🚀 Запуск решателя капчи...");
  const rustPath = getRustPath();
  console.log(`📂 Using Rust binary at: ${rustPath}`);

  try {
    rustProcess = spawn(rustPath);
    rustStdin = rustProcess.stdin;

    rustStdin.write(JSON.stringify({ api_key: global.apiKey }) + '\n');

    setTimeout(() => requestNewTask(), 1000);

    rustProcess.stdout.on('data', (data) => {
      try {
        const task = JSON.parse(data.toString().trim());
        console.log("📦 Получено задание:", task);

        if (!task.url || !task.sitekey) {
          console.log("ℹ️ Не задача или неполная задача:", task);
          return;
        }

        const captchaWin = new BrowserWindow({
          width: 1000,
          height: 800,
          show: false,
          webPreferences: {
            preload: path.join(__dirname, 'preload.js'),
            contextIsolation: true,
            devTools: true
          }
        });

        session.defaultSession.webRequest.onHeadersReceived((details, callback) => {
          const headers = details.responseHeaders;
          delete headers['content-security-policy'];
          delete headers['content-security-policy-report-only'];
          callback({ responseHeaders: headers });
        });

        captchaWin.loadURL(task.url);
        captchaWin.webContents.once('did-finish-load', () => {
          captchaWin.webContents.send('task', task);
          captchaWin.show();
        });
      } catch (e) {
        console.error("❌ Ошибка парсинга от Rust:", e);
      }
    });

    rustProcess.stderr.on('data', (data) => {
      console.error("RUST STDERR:", data.toString());
    });

    rustProcess.on('exit', (code) => {
      console.log(`Rust завершён с кодом ${code}`);
      rustProcess = null;
      rustStdin = null;
    });
    
    rustProcess.on('error', (err) => {
      console.error("❌ Ошибка запуска Rust процесса:", err);
      rustProcess = null;
      rustStdin = null;
    });
  } catch (err) {
    console.error("❌ Critical error:", err);
    rustProcess = null;
    rustStdin = null;
  }
}

function requestNewTask() {
  if (rustStdin && rustStdin.writable) {
    console.log("📬 Запрос новой задачи");
    rustStdin.write(JSON.stringify({ command: "get_task" }) + '\n');
  } else {
    console.warn("⚠️ Rust stdin не активен, не могу запросить задачу");
  }
}

ipcMain.on('captcha:solved', (_event, solution) => {
  if (!rustStdin || !rustStdin.writable) {
    console.error("❌ Rust stdin не доступен");
    return;
  }

  console.log("📤 Отправка решения в Rust:", solution);
  rustStdin.write(JSON.stringify({
    command: "submit_solution",
    ...solution
  }) + '\n');

  mainWin.loadFile('menu.html');

  setTimeout(() => {
    requestNewTask();
  }, 1000);
});