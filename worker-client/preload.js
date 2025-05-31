const { ipcRenderer } = require('electron');

window.ipcRenderer = ipcRenderer;

window.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'captcha-solved') {
    ipcRenderer.send('captcha:solved', event.data);
  } else if (event.data && event.data.type === 'return-menu') {
    ipcRenderer.send('close:captcha');
  }
});

window.getBalance = () => ipcRenderer.invoke('get:balance');