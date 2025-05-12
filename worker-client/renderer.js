// Обработка получения задачи
ipcRenderer.on('task-received', (event, task) => {
    console.log('📦 Получено задание:', task);
    
    // Загружаем капчу в отдельном окне
    ipcRenderer.invoke('load-captcha', {
        url: task.url,
        sitekey: task.sitekey,
        type: task.type
    }).then(success => {
        if (!success) {
            console.error('❌ Ошибка загрузки капчи');
        }
    });
});

// Обработка решения капчи
ipcRenderer.on('captcha-solution', (event, token) => {
    // Отправляем решение на сервер
    const solution = {
        command: 'submit_solution',
        task_id: currentTaskId,
        solution: token
    };
    
    // Отправляем решение через IPC в main процесс
    ipcRenderer.send('submit-solution', solution);
});

// Обработка ошибок
ipcRenderer.on('error', (event, error) => {
    console.error('❌ Ошибка:', error);
}); 