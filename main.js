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
  const rust = spawn(rustPath);

  rust.stdin.write(JSON.stringify({ api_key: inputKey }) + '\n');

  return new Promise((resolve) => {
    let output = '';

    rust.stdout.on('data', (data) => {
      output += data.toString();
    });

    rust.stderr.on('data', (data) => {
      console.error("RUST STDERR:", data.toString());
    });

    rust.on('close', (code) => {
      try {
        const response = JSON.parse(output.trim());

        if (response.sitekey && response.url) {
          apiKey = inputKey;
          mainWin.loadFile('menu.html');
          resolve({ ok: true, balance: 0 });
        } else if (response.status === 'error') {
          resolve({ ok: false, message: response.message || 'Invalid key' });
        } else {
          resolve({ ok: false, message: 'Unrecognized response' });
        }
      } catch (e) {
        if (output.trim().toLowerCase().includes('invalid')) {
          resolve({ ok: false, message: 'Invalid key' });
        } else {
          resolve({ ok: false, message: 'Bad response from Rust' });
        }
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

  rl.on('line', (line) => {
    try {
      const task = JSON.parse(line.trim());
      console.log("📦 Получено задание:", task);

      if (!captchaWin) {
        captchaWin = new BrowserWindow({
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

        captchaWin.once('ready-to-show', () => captchaWin.show());
      }

      captchaWin.loadURL(task.url);
      captchaWin.webContents.once('did-finish-load', () => {
        captchaWin.webContents.send('task', task);
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
}

ipcMain.on('captcha:solved', (_event, solution) => {
  if (!rustStdin || !rustStdin.writable) {
    console.error("❌ Rust stdin не доступен");
    return;
  }

  console.log("📤 Отправка решения в Rust:", solution);
  rustStdin.write(JSON.stringify(solution) + '\n');
});

function requestNewTask() {
  if (rustStdin && rustStdin.writable) {
    rustStdin.write('\n'); // триггер следующей задачи, если реализовано в Rust
  } else {
    console.warn("⚠️ Rust stdin не активен, не могу запросить задачу");
  }
}