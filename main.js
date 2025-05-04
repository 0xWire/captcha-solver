const { app, BrowserWindow, ipcMain, session } = require('electron');
const path = require('path');
const { spawn } = require('child_process');
const axios = require('axios');

let mainWin;
let apiKey = null; // Store API key

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

// 🚀 Запускаем с окна авторизации
app.whenReady().then(() => {
  createWindow('auth.html');
});

ipcMain.handle('auth:login', async (_event, apiKey) => {
  console.log("🔐 Получен ключ:", apiKey);

  // Отправляем на Go-сервер (localhost)
  try {
    const res = await axios.post('http://127.0.0.1:8080/auth', { api_key: apiKey });
    if (res.data.status === "ok") {
      console.log("✅ Авторизация успешна");
      
      // Store API key for later use
      global.apiKey = apiKey;

      // Переход в главное меню
      mainWin.loadFile('menu.html');
      return { ok: true, balance: res.data.balance || 123.45 };
    } else {
      console.log("❌ Неверный ключ");
      return { ok: false };
    }
  } catch (err) {
    console.error("⚠️ Ошибка запроса:", err);
    return { ok: false };
  }
});

ipcMain.on('menu:solve', () => {
  runCaptchaSolver();
});

function runCaptchaSolver() {
  // Check if we have API key
  if (!global.apiKey) {
    console.error("❌ API ключ не найден!");
    return;
  }

  console.log("🚀 Запуск решателя капчи...");
  
  const rustPath = path.join(process.resourcesPath || '.', 'captcha_cli');
  console.log(`📂 Путь к исполняемому файлу: ${rustPath}`);
  
  const rust = spawn(rustPath);
  
  // Create API key payload
  const authPayload = JSON.stringify({ api_key: global.apiKey });
  
  // Write to stdin - this is what the Rust process is waiting for
  rust.stdin.write(authPayload + '\n');
  rust.stdin.end();
  
  console.log(`✅ API ключ отправлен в Rust процесс`);

  rust.stdout.on('data', (data) => {
    try {
      const task = JSON.parse(data.toString().trim());
      console.log("📦 Получено задание:", task);

      // Загружаем страницу с капчей
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

      // Удаляем CSP
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

  rust.stderr.on('data', (data) => {
    console.error("RUST STDERR:", data.toString());
  });

  rust.on('exit', (code) => {
    console.log(`Rust завершён с кодом ${code}`);
  });
  
  rust.on('error', (err) => {
    console.error("❌ Ошибка запуска Rust процесса:", err);
  });
}

