const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electron', {
  invoke: (channel, data) => {
    const validChannels = ['captcha-solved', 'auth:login'];
    if (validChannels.includes(channel)) {
      return ipcRenderer.invoke(channel, data);
    }
  },
  send: (channel, data) => {
    const validChannels = ['menu:solve', 'captcha:solved', 'submit-solution'];
    if (validChannels.includes(channel)) {
      ipcRenderer.send(channel, data);
    }
  },
  getBalance: () => ipcRenderer.invoke('get:balance'),
  // Add developer tools log helper
  debug: (message) => {
    console.log(`[DEBUG]: ${message}`);
  }
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

  ipcRenderer.on('task', (event, task) => {
    console.log("📩 Получено задание:", task);

    try {
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
          type: task.type,
          task_id: task.task_id
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
