const { app, BrowserWindow, ipcMain, session } = require('electron');
const path = require('path');
const { spawn } = require('child_process');
const readline = require('readline');

let mainWin;
let apiKey = null;
let captchaWin = null;
let rustProcess = null;
let rustStdin = null;

function createWindow(htmlFile) {
  mainWin = new BrowserWindow({
    width: 800,
    height: 600,
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false
    }
  });

  mainWin.loadFile(htmlFile);
  mainWin.once('ready-to-show', () => mainWin.show());
}

app.whenReady().then(() => {
  createWindow('auth.html');
});

ipcMain.handle('auth:login', async (_event, inputKey) => {
  const rustPath = path.join(process.resourcesPath || '.', 'captcha_cli');
  const rust = spawn(rustPath, ['auth']);

  rust.stdin.write(JSON.stringify({ api_key: inputKey }) + '\n');
  rust.stdin.end();

  return new Promise((resolve) => {
    let output = '';

    rust.stdout.on('data', (data) => {
      output += data.toString();
    });

    rust.stderr.setEncoding('utf-8');
    rust.stderr.on('data', (data) => {
      console.error("RUST STDERR:", data);
    });

    rust.on('close', () => {
      try {
        const response = JSON.parse(output.trim());
        if (response.status === 'ok') {
          apiKey = inputKey;
          mainWin.loadFile('menu.html');
          resolve({ ok: true, balance: response.balance || 0 });
        } else {
          resolve({ ok: false, message: response.message || 'Invalid key' });
        }
      } catch (e) {
        resolve({ ok: false, message: `Bad response from Rust. Error: ${e}` });
      }
    });
  });
});

ipcMain.handle('get:balance', async () => {
  return { ok: true, balance: 0 };
});

ipcMain.on('menu:solve', () => {
  if (!rustProcess) {
    startRustSolver();
  } else {
    requestNewTask();
  }
});

function startRustSolver() {
  if (!apiKey) {
    console.error("❌ API ключ не найден!");
    return;
  }

  const rustPath = path.join(process.resourcesPath || '.', 'captcha_cli');
  rustProcess = spawn(rustPath);
  rustStdin = rustProcess.stdin;

  const rl = readline.createInterface({ input: rustProcess.stdout });

  rustStdin.write(JSON.stringify({ api_key: apiKey }) + '\n');
  setTimeout(() => requestNewTask(), 300); // делаем первую команду позже

  rl.on('line', (line) => {
    try {
      const parsed = JSON.parse(line.trim());

      if (parsed.status && parsed.status !== 'ok' && parsed.status !== 'solution_saved') {
        console.log("ℹ️ Ответ не является задачей, пропускаем", parsed);
        return;
      }

      if (!parsed.url || !parsed.sitekey) {
        console.log("ℹ️ Не задача, пропускаем", parsed);
        return;
      }

      const task = parsed;
      console.log("📦 Получено задание:", task);

      if (!captchaWin) {
        captchaWin = new BrowserWindow({
          width: 1000,
          height: 800,
          show: false,
          frame: false,
          transparent: true,
          autoHideMenuBar: true,
          webPreferences: {
            preload: path.join(__dirname, 'preload.js'),
            contextIsolation: true,
            nodeIntegration: false
          }
        });

        captchaWin.once('ready-to-show', () => captchaWin.show());

        captchaWin.webContents.on('will-navigate', e => e.preventDefault());
        session.defaultSession.webRequest.onHeadersReceived((details, callback) => {
          const headers = details.responseHeaders;
          delete headers['content-security-policy'];
          delete headers['content-security-policy-report-only'];
          callback({ responseHeaders: headers });
        });
      }

      captchaWin.loadURL(task.url);
      captchaWin.webContents.once('did-finish-load', () => {
        captchaWin.webContents.executeJavaScript(`
          document.body.innerHTML = '';
          document.body.style.background = '#0f172a';
        `).then(() => {
          captchaWin.webContents.send('task', task);
        });
      });

    } catch (e) {
      console.error("❌ Ошибка парсинга задания:", e);
    }
  });

  rustProcess.stderr.setEncoding('utf-8');
  rustProcess.stderr.on('data', (data) => {
    console.error("RUST STDERR:", data);
  });

  rustProcess.on('exit', (code) => {
    console.log(`Rust завершён с кодом ${code}`);
    rustProcess = null;
    rustStdin = null;
  });
}

ipcMain.on('captcha:solved', (_event, solution) => {
  if (!rustStdin || !rustStdin.writable) {
    console.error("❌ Rust stdin не доступен");
    return;
  }

  console.log("📤 Отправка решения в Rust:", solution);
  rustStdin.write(JSON.stringify({
    ...solution,
    command: "submit_solution"
  }) + '\n');

  setTimeout(() => {
    requestNewTask();
  }, 500);
});

function requestNewTask() {
  if (rustStdin && rustStdin.writable) {
    rustStdin.write(JSON.stringify({ command: "get_task" }) + '\n');
  } else {
    console.warn("⚠️ Rust stdin не активен, не могу запросить задачу");
  }
}