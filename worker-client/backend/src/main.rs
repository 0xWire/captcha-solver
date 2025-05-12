use tokio::io::{self, AsyncBufReadExt, BufReader};
use async_tungstenite::tokio::connect_async;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::env;
use std::fs::File;
use std::io::Write;

#[derive(Serialize, Deserialize, Debug)]
struct Task {
    url: String,
    sitekey: String,
    #[serde(rename = "type")]
    kind: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    task_id: Option<i64>,
}

#[derive(Serialize, Deserialize, Debug)]
struct AuthPayload {
    api_key: String,
    #[serde(default)]
    command: Option<String>,
}

#[derive(Serialize, Deserialize, Debug)]
struct CaptchaSolution {
    token: String,
    url: String,
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    command: Option<String>,
    #[serde(default)]
    task_id: Option<i64>,
    #[serde(default)]
    solution: Option<String>,
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
    eprintln!("🔐 Авторизация");

    let stdin = io::stdin();
    let mut lines = BufReader::new(stdin).lines();

    let Ok(Some(line)) = lines.next_line().await else {
        println!("{}", json!({ "status": "error", "message": "no_input" }));
        return;
    };

    let Ok(auth): Result<AuthPayload, _> = serde_json::from_str(&line) else {
        println!("{}", json!({ "status": "error", "message": "invalid_json" }));
        return;
    };

    eprintln!("🌐 Подключение к WebSocket...");
    let (ws_stream, _) = match connect_async("ws://127.0.0.1:8080/socket").await {
        Ok(pair) => pair,
        Err(e) => {
            println!("{}", json!({ "status": "error", "message": format!("connection error: {}", e) }));
            return;
        }
    };

    let (mut write, mut read) = ws_stream.split();

    // Отправка API ключа
    let Ok(auth_json) = serde_json::to_string(&auth) else {
        println!("{}", json!({ "status": "error", "message": "serialization error" }));
        return;
    };

    if let Err(e) = write.send(auth_json.into()).await {
        println!("{}", json!({ "status": "error", "message": format!("send error: {}", e) }));
        return;
    }

    // Ждем ответ от сервера
    if let Some(Ok(msg)) = read.next().await {
        if msg.is_text() {
            println!("{}", msg.to_text().unwrap_or(""));
        } else {
            println!("{}", json!({ "status": "error", "message": "invalid response format" }));
        }
    } else {
        println!("{}", json!({ "status": "error", "message": "no response from server" }));
    }
}

async fn run_task_mode() {
    eprintln!("📦 Режим задач");

    let stdin = io::stdin();
    let mut lines = BufReader::new(stdin).lines();

    // Читаємо API ключ
    let Ok(Some(first_line)) = lines.next_line().await else {
        eprintln!("❌ Не получен API ключ");
        return;
    };

    let Ok(auth): Result<AuthPayload, _> = serde_json::from_str(&first_line) else {
        eprintln!("❌ Неверный JSON авторизации");
        return;
    };

    let api_key = auth.api_key;

    eprintln!("🌐 Подключение к WebSocket...");
    let (ws_stream, _) = match connect_async("ws://127.0.0.1:8080/socket").await {
        Ok(pair) => pair,
        Err(e) => {
            eprintln!("❌ Не удалось подключиться: {e}");
            return;
        }
    };

    let (mut write, mut read) = ws_stream.split();

    // Отправка API ключа
    let Ok(auth_json) = serde_json::to_string(&AuthPayload { api_key: api_key.clone(), command: None }) else {
        eprintln!("❌ Ошибка сериализации ключа");
        return;
    };

    if let Err(e) = write.send(auth_json.into()).await {
        eprintln!("❌ Ошибка отправки ключа: {e}");
        return;
    }

    // Ждем ответ авторизации
    let auth_response = match read.next().await {
        Some(Ok(msg)) if msg.is_text() => {
            let text = msg.to_text().unwrap_or("").to_string();
            text
        },
        _ => {
            eprintln!("❌ Не получен ответ авторизации");
            return;
        }
    };

    // Проверяем успешность авторизации
    let auth_result: serde_json::Value = match serde_json::from_str(&auth_response) {
        Ok(val) => val,
        Err(_) => {
            eprintln!("❌ Ошибка парсинга ответа авторизации");
            return;
        }
    };

    if auth_result.get("status") != Some(&json!("ok")) {
        eprintln!("❌ Ошибка авторизации: {}", auth_result.get("message").unwrap_or(&json!("unknown error")));
        return;
    }

    eprintln!("✅ Авторизация успешна");

    loop {
        let Ok(Some(line)) = lines.next_line().await else {
            eprintln!("🔚 Stdin закрыт");
            break;
        };

        if line.trim().is_empty() {
            continue;
        }

        let parsed: serde_json::Value = match serde_json::from_str(&line) {
            Ok(val) => val,
            Err(e) => {
                eprintln!("⚠️ Невалидный JSON: {} ({})", line, e);
                continue;
            }
        };

        if parsed.get("command") == Some(&json!("get_task")) {
            let cmd = json!({ "command": "get_task" });
            if let Err(e) = write.send(cmd.to_string().into()).await {
                eprintln!("❌ Ошибка запроса задачи: {e}");
                break;
            }

            // ждем ответ
            match read.next().await {
                Some(Ok(msg)) if msg.is_text() => {
                    let response = msg.to_text().unwrap_or("");
                    if !response.is_empty() {
                        // Проверка валидности JSON перед выводом
                        if let Ok(_) = serde_json::from_str::<serde_json::Value>(response) {
                            println!("{}", response);
                        } else {
                            eprintln!("⚠️ Получен невалидный JSON от сервера");
                            println!("{}", json!({
                                "status": "error",
                                "message": "invalid_server_response"
                            }));
                        }
                    } else {
                        println!("{}", json!({
                            "status": "error",
                            "message": "empty_response"
                        }));
                    }
                },
                _ => {
                    eprintln!("⚠️ Не получен ответ на запрос задачи");
                    println!("{}", json!({
                        "status": "error",
                        "message": "no_response"
                    }));
                }
            }

        } else if parsed.get("command") == Some(&json!("submit_solution")) {
            if let Err(e) = write.send(line.into()).await {
                eprintln!("❌ Ошибка отправки решения: {e}");
                break;
            }

            // ответ о сохранении решения
            match read.next().await {
                Some(Ok(msg)) if msg.is_text() => {
                    let response = msg.to_text().unwrap_or("");
                    if !response.is_empty() {
                        // Проверка валидности JSON перед выводом
                        if let Ok(_) = serde_json::from_str::<serde_json::Value>(response) {
                            println!("{}", response);
                        } else {
                            eprintln!("⚠️ Получен невалидный JSON от сервера при сохранении");
                            println!("{}", json!({
                                "status": "error",
                                "message": "invalid_server_response"
                            }));
                        }
                    } else {
                        println!("{}", json!({
                            "status": "error",
                            "message": "empty_response"
                        }));
                    }
                },
                _ => {
                    eprintln!("⚠️ Не получен ответ на отправку решения");
                    println!("{}", json!({
                        "status": "error",
                        "message": "no_response"
                    }));
                }
            }

        } else {
            eprintln!("⚠️ Неизвестная команда: {}", line);
        }
    }

    eprintln!("👋 Завершение CLI");
}