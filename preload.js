const { contextBridge, ipcRenderer } = require('electron');

// Прокидываем ipc методы в window.electron
contextBridge.exposeInMainWorld('electron', {
  invoke: ipcRenderer.invoke,
  send: ipcRenderer.send,
  on: (channel, callback) => {
    ipcRenderer.on(channel, (event, ...args) => callback(...args));
  },
  getBalance: () => ipcRenderer.invoke('get:balance')
});

console.log("👀 Preload script загружен");

// Add CSS to hide scrollbars
window.addEventListener('DOMContentLoaded', () => {
  console.log("📦 DOM готов");
  
  // Add CSS to hide scrollbars
  const style = document.createElement('style');
  style.textContent = `
    ::-webkit-scrollbar {
      display: none;
    }
    
    body {
      -ms-overflow-style: none;  /* IE and Edge */
      scrollbar-width: none;  /* Firefox */
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

      window.onCaptchaSolved = function(token) {
        console.log("✅ Капча решена:", token);
        fetch('http://127.0.0.1:8080/captcha_token', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token, url: task.url, type: task.type })
        }).catch(err => console.error("❌ Ошибка отправки токена:", err));
      };

    } catch (e) {
      console.error("❌ Ошибка при вставке капчи:", e);
    }
  });
});
