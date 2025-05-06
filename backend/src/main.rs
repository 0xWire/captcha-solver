use tokio::io::{self, AsyncBufReadExt, BufReader};
use async_tungstenite::tokio::connect_async;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use std::env;

#[derive(Serialize, Deserialize, Debug)]
struct Task {
    url: String,
    sitekey: String,
    #[serde(rename = "type")]
    kind: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct AuthPayload {
    api_key: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct CaptchaSolution {
    token: String,
    url: String,
    #[serde(rename = "type")]
    kind: String,
}

#[tokio::main]
async fn main() {
    let mut args = env::args().skip(1);

    if let Some(arg) = args.next() {
        if arg == "auth" {
            run_auth_mode().await;
            return;
        }
    }

    run_task_mode().await;
}

async fn run_auth_mode() {
    eprintln!("🔐 Режим авторизации");

    let stdin = io::stdin();
    let mut lines = BufReader::new(stdin).lines();

    let Some(Ok(line)) = lines.next_line().await else {
        eprintln!("❌ Не удалось прочитать ключ");
        std::process::exit(1);
    };

    let Ok(auth): Result<AuthPayload, _> = serde_json::from_str(&line) else {
        eprintln!("❌ Неверный JSON");
        std::process::exit(1);
    };

    let client = reqwest::Client::new();
    match client.post("http://127.0.0.1:8080/auth")
        .json(&auth)
        .send()
        .await
    {
        Ok(res) => {
            let text = res.text().await.unwrap_or_else(|_| "{}".into());
            println!("{}", text);
        },
        Err(e) => {
            eprintln!("❌ Ошибка HTTP: {e}");
            std::process::exit(1);
        }
    }
}

async fn run_task_mode() {
    eprintln!("📦 Основной режим");
    let stdin = io::stdin();
    let mut lines = BufReader::new(stdin).lines();

    eprintln!("⏳ Ожидаем API ключ...");
    let Some(Ok(line)) = lines.next_line().await else {
        eprintln!("❌ Не удалось прочитать API ключ");
        return;
    };

    let Ok(auth): Result<AuthPayload, _> = serde_json::from_str(&line) else {
        eprintln!("❌ Неверный JSON для API ключа");
        return;
    };

    // Подключение к WebSocket
    eprintln!("🌐 Подключение к WebSocket...");
    let (ws_stream, _) = match connect_async("ws://127.0.0.1:8080/socket").await {
        Ok(pair) => pair,
        Err(e) => {
            eprintln!("❌ Не удалось подключиться: {e}");
            return;
        }
    };

    eprintln!("✅ WebSocket подключен");

    let (mut write, mut read) = ws_stream.split();

    // Отправка API ключа
    let Ok(auth_json) = serde_json::to_string(&auth) else {
        eprintln!("❌ Ошибка сериализации ключа");
        return;
    };

    if let Err(e) = write.send(auth_json.into()).await {
        eprintln!("❌ Ошибка отправки ключа: {e}");
        return;
    }

    eprintln!("📤 Ключ отправлен. Ожидаем задачу...");

    // Ожидаем задачу
    while let Some(msg) = read.next().await {
        let Ok(msg) = msg else {
            eprintln!("❌ Ошибка чтения WebSocket");
            break;
        };

        if msg.is_text() {
            let Ok(task): Result<Task, _> = serde_json::from_str(&msg.to_text().unwrap_or("")) else {
                eprintln!("❌ Неверный формат задачи");
                continue;
            };

            // Отправляем в stdout
            println!("{}", serde_json::to_string(&task).unwrap());
            eprintln!("✅ Задача отправлена в stdout. Ожидаем решение...");

            // Читаем решение из stdin
            let Some(Ok(line)) = lines.next_line().await else {
                eprintln!("❌ Не получено решение из stdin");
                break;
            };

            let Ok(solution): Result<CaptchaSolution, _> = serde_json::from_str(&line) else {
                eprintln!("❌ Неверный JSON в решении");
                break;
            };

            let Ok(solution_json) = serde_json::to_string(&solution) else {
                eprintln!("❌ Не удалось сериализовать решение");
                break;
            };

            // Отправка решения обратно в WebSocket
            if let Err(e) = write.send(solution_json.into()).await {
                eprintln!("❌ Не удалось отправить решение: {e}");
                break;
            }

            eprintln!("📤 Решение отправлено. Завершение...");
            break;
        }
    }

    eprintln!("👋 Rust CLI завершает работу");
}