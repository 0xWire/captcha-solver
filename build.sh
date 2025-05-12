#!/bin/bash

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Функция для вывода сообщений
log() {
    echo -e "${GREEN}[BUILD]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Проверяем наличие необходимых инструментов
if ! command -v cargo &> /dev/null; then
    error "Cargo не установлен. Установите Rust и Cargo: https://rustup.rs/"
    exit 1
fi

if ! command -v npm &> /dev/null; then
    error "npm не установлен. Установите Node.js: https://nodejs.org/"
    exit 1
fi

# Сборка Rust бэкенда
log "Сборка Rust бэкенда..."
cd worker-client/backend || {
    error "Не удалось перейти в директорию worker-client/backend"
    exit 1
}

# Очищаем предыдущую сборку
#log "Очистка предыдущей сборки..."
#cargo clean

# Собираем релизную версию
log "Сборка релизной версии..."
cargo build --release

# Проверяем успешность сборки
if [ $? -eq 0 ]; then
    log "Сборка Rust успешно завершена!"
    log "Бинарник находится в: $(pwd)/target/release/captcha-secure-client"
else
    error "Ошибка при сборке Rust!"
    exit 1
fi

# Возвращаемся в директорию worker-client
cd ..

# Подготавливаем директорию для бинарника
#log "Подготовка директории для бинарника..."
#mkdir -p dist/linux-unpacked/resources

# Копируем бинарник в нужную директорию
log "Копирование бинарника в Electron приложение..."
cp backend/target/release/captcha_cli dist/captcha_cli
chmod +x dist/captcha_cli

# Сборка Electron приложения
log "Сборка Electron приложения..."
npm install
npm run build

# Проверяем успешность сборки Electron
if [ $? -eq 0 ]; then
    log "Сборка Electron успешно завершена!"
else
    error "Ошибка при сборке Electron!"
    exit 1
fi

# Возвращаемся в корневую директорию
cd ..

log "Готово! Приложение собрано и готово к использованию."
log "Бинарник находится в: worker-client/dist/linux-unpacked/resources/captcha_cli" 