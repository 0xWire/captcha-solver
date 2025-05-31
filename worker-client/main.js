const { app, BrowserWindow, ipcMain, session, globalShortcut, Menu } = require('electron');
const path = require('path');
const { spawn } = require('child_process');

let mainWin;
let captchaWin = null;
let rustProcess = null;
let rustStdin = null;

function getRustPath() {
  return process.platform === 'win32'
    ? path.join(process.resourcesPath, 'captcha_cli.exe')
    : path.join(process.resourcesPath, 'captcha_cli');
}

function createWindow(htmlFile) {
  mainWin = new BrowserWindow({
    width: 800,
    height: 600,
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: false,
      nodeIntegration: true,
      devTools: true
    }
  });

  const contextMenu = Menu.buildFromTemplate([
    { label: 'Inspect element', click: () => mainWin.webContents.inspectElement(0, 0) },
    { label: 'Open developer tools', click: () => mainWin.webContents.toggleDevTools() }
  ]);

  mainWin.webContents.on('context-menu', () => {
    contextMenu.popup();
  });

  mainWin.loadFile(htmlFile);
  mainWin.once('ready-to-show', () => {
    mainWin.show();
    mainWin.webContents.openDevTools({ mode: 'detach' });
  });
}

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

  createWindow('auth.html');
});

app.on('will-quit', () => {
  globalShortcut.unregisterAll();
});

ipcMain.handle('auth:login', async (_event, apiKey) => {
  console.log("🔐 Got API key:", apiKey);
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
          console.error("❌ Parsing error:", e);
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
    return { ok: false, message: 'API key not found' };
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
        resolve({ ok: false, message: 'Rust error' });
      });

      rust.on('close', () => {
        try {
          const response = JSON.parse(output.trim());
          if (response.status === 'ok') {
            resolve({ ok: true, balance: response.balance });
          } else {
            resolve({ ok: false, message: response.message });
          }
        } catch (e) {
          console.error("❌ Parsing error:", e);
          resolve({ ok: false, message: 'Parsing error' });
        }
      });
    });
  } catch (err) {
    console.error("❌ Critical error:", err);
    return { ok: false, message: 'Failed to get balance' };
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
    console.error("❌ API key not found!");
    return;
  }

  console.log("🚀 Starting captcha solver...");
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
        console.log("📦 Got task:", task);

        if (!task.url || !task.sitekey) {
          console.log("ℹ️ No task or incomplete task:", task);
          return;
        }

        openCaptchaWindow(task);

      } catch (e) {
        console.error("❌ Rust parsing error:", e);
      }
    });

    rustProcess.stderr.on('data', (data) => {
      console.error("RUST STDERR:", data.toString());
    });

    rustProcess.on('exit', (code) => {
      console.log(`Rust exited with code ${code}`);
      rustProcess = null;
      rustStdin = null;
    });

    rustProcess.on('error', (err) => {
      console.error("❌ Rust process error:", err);
      rustProcess = null;
      rustStdin = null;
    });
  } catch (err) {
    console.error("❌ Critical error:", err);
    rustProcess = null;
    rustStdin = null;
  }
}

function openCaptchaWindow(task) {
  captchaWin = new BrowserWindow({
    width: 1000,
    height: 800,
    show: false,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: false,
      nodeIntegration: true,
      devTools: true
    }
  });

  captchaWin.loadURL(task.url);

  captchaWin.webContents.once('did-finish-load', () => {
    captchaWin.show();
    injectRecaptcha(captchaWin, task.sitekey, task.task_id);
  });

  captchaWin.on('closed', () => {
    captchaWin = null;
  });
}

function requestNewTask() {
  if (rustStdin && rustStdin.writable) {
    console.log("📬 Requesting new task");
    rustStdin.write(JSON.stringify({ command: "get_task" }) + '\n');
  } else {
    console.warn("⚠️ Rust stdin is not active, cannot request task");
  }
}

function injectRecaptcha(win, sitekey, taskId) {
  const script = `
    if (!document.getElementById('injected-recaptcha-overlay')) {
      const overlay = document.createElement('div');
      overlay.id = 'injected-recaptcha-overlay';
      overlay.style = \`
        position: fixed;
        top: 0; left: 0;
        width: 100%; height: 100%;
        background: #0f172a;
        z-index: 99998;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: flex-start;
        pointer-events: all;
        padding-top: 20px;
      \`;

      const button = document.createElement('button');
      button.innerText = 'Return to main menu';
      button.style = \`
        margin-bottom: 20px;
        padding: 10px 20px;
        font-size: 14px;
        border-radius: 5px;
        border: none;
        cursor: pointer;
      \`;
      button.onclick = () => {
        window.postMessage({ type: 'return-menu' }, '*');
      };

      const wrapper = document.createElement('div');
      wrapper.id = 'injected-recaptcha';
      wrapper.style = \`
        background: #fff;
        padding: 20px;
        border-radius: 10px;
        box-shadow: 0 0 20px rgba(0,0,0,0.5);
        z-index: 99999;
        display: flex;
        flex-direction: column;
        align-items: center;
      \`;

      const title = document.createElement('div');
      title.innerText = 'Solve the captcha';
      title.style = \`
        font-size: 20px;
        font-weight: bold;
        color: #0f172a;
        margin-bottom: 12px;
      \`;

      const captchaDiv = document.createElement('div');
      captchaDiv.className = 'g-recaptcha';
      captchaDiv.setAttribute('data-sitekey', '${sitekey}');
      captchaDiv.setAttribute('data-callback', 'onCaptchaSolved');

      overlay.appendChild(button);
      wrapper.appendChild(title);
      wrapper.appendChild(captchaDiv);
      overlay.appendChild(wrapper);
      document.body.appendChild(overlay);

      const recaptchaScript = document.createElement('script');
      recaptchaScript.src = 'https://www.google.com/recaptcha/api.js';
      document.body.appendChild(recaptchaScript);

      window.onCaptchaSolved = function(token) {
        console.log("✅ [Injected] Captcha solved, token:", token);
        window.postMessage({ type: 'captcha-solved', token, task_id: ${taskId} }, '*');
        const overlay = document.getElementById('injected-recaptcha-overlay');
        if (overlay) overlay.remove();
      };

      console.log("✅ Captcha injected with return button");
    }
  `;

  win.webContents.executeJavaScript(script).catch(err => {
    console.error("❌ Error injecting captcha:", err);
  });
}

ipcMain.on('captcha:solved', (_event, solution) => {
  console.log("✅ [MAIN] Captcha token received:", solution.token);

  if (rustStdin && rustStdin.writable) {
    rustStdin.write(JSON.stringify({
      command: "submit_solution",
      task_id: solution.task_id,
      solution: solution.token
    }) + '\n');

    console.log("📨 Sent solution to Rust, requesting next task...");

    if (captchaWin) {
      captchaWin.close();
      captchaWin = null;
    }

    setTimeout(() => {
      requestNewTask();
    }, 500);
  } else {
    console.warn("⚠️ Rust stdin is not writable, cannot send solution");
    mainWin.loadFile('menu.html');
  }
});

ipcMain.on('close:captcha', () => {
  if (captchaWin) {
    console.log("🔙 Closing captcha window, returning to main menu");
    captchaWin.close();
    captchaWin = null;
  }
});