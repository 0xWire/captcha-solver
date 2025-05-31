const { ipcRenderer } = require('electron');

window.ipcRenderer = ipcRenderer;

window.getBalance = () => ipcRenderer.invoke('get:balance');

window.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'captcha-solved') {
    console.log("✅ [Preload] Got token:", event.data.token);
    ipcRenderer.send('captcha:solved', { token: event.data.token });
  }
});