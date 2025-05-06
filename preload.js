const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electron', {
  invoke: ipcRenderer.invoke,
  send: ipcRenderer.send,
  on: (channel, callback) => {
    ipcRenderer.on(channel, (event, ...args) => callback(...args));
  },
  getBalance: () => ipcRenderer.invoke('get:balance')
});

console.log("👀 Preload script загружен");

window.addEventListener('DOMContentLoaded', () => {
  console.log("📦 DOM готов");

  const style = document.createElement('style');
  style.textContent = `
    ::-webkit-scrollbar {
      display: none;
    }
    body {
      -ms-overflow-style: none;
      scrollbar-width: none;
      overflow: hidden;
    }
  `;
  document.head.appendChild(style);

  ipcRenderer.on('task', (_event, task) => {
    console.log("📩 Получено задание:", task);

    try {
      const old = document.getElementById("captcha-wrapper");
      if (old) old.remove();

      const wrapper = document.createElement('div');
      wrapper.id = "captcha-wrapper";
      wrapper.style = `
        position: fixed;
        inset: 0;
        z-index: 999999;
        background: #0f172a;
        display: flex;
        align-items: center;
        justify-content: center;
      `;

      wrapper.innerHTML = `
        <div style="background: white; padding: 20px; border-radius: 10px; box-shadow: 0 0 20px rgba(0,0,0,0.3);">
          <h2 style="text-align:center; margin-bottom: 16px;">Решите капчу</h2>
          <div class="g-recaptcha" data-sitekey="${task.sitekey}" data-callback="onCaptchaSolved"></div>
        </div>
      `;

      document.body.appendChild(wrapper);

      const script = document.createElement('script');
      script.src = 'https://www.google.com/recaptcha/api.js';
      script.onload = () => console.log("✅ Капча загружена");
      script.onerror = () => console.error("❌ Не удалось загрузить капчу");
      document.body.appendChild(script);

      // глобальна функція виклику після вирішення капчі
      window.onCaptchaSolved = function(token) {
        console.log("✅ Капча решена:", token);

        ipcRenderer.send('captcha:solved', {
          token,
          url: task.url,
          type: task.type
        });

        // прибираємо UI
        const wrap = document.getElementById("captcha-wrapper");
        if (wrap) wrap.remove();
      };

    } catch (e) {
      console.error("❌ Ошибка при вставке капчи:", e);
    }
  });
});