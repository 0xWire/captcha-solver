use tokio::io::{self, AsyncBufReadExt, BufReader};
use async_tungstenite::tokio::connect_async;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};

#[derive(Deserialize, Serialize)]
struct Task {
    url: String,
    sitekey: String,
    #[serde(rename = "type")]
    kind: String,
}

#[derive(Serialize, Deserialize)]
struct AuthPayload {
    api_key: String,
}

#[tokio::main]
async fn main() {
    eprintln!("📦 Rust CLI запущен");
    
    let stdin = io::stdin();
    let mut lines = BufReader::new(stdin).lines();

    eprintln!("⏳ Ожидаем API ключ из stdin...");
    
    let line = match lines.next_line().await {
        Ok(Some(line)) => line,
        Ok(None) => {
            eprintln!("❌ Пустой ввод из stdin!");
            return;
        }
        Err(e) => {
            eprintln!("❌ Ошибка чтения из stdin: {}", e);
            return;
        }
    };

    eprintln!("🔑 Получен ввод из stdin: {}", line);
    
    let auth: AuthPayload = match serde_json::from_str(&line) {
        Ok(auth) => auth,
        Err(e) => {
            eprintln!("❌ Ошибка парсинга JSON: {}", e);
            return;
        }
    };

    eprintln!("🌐 Подключение к WebSocket...");
    
    let (ws_stream, _) = match connect_async("ws://127.0.0.1:8080/socket").await {
        Ok(conn) => conn,
        Err(e) => {
            eprintln!("❌ Ошибка подключения к WebSocket: {}", e);
            return;
        }
    };

    eprintln!("✅ Подключено к WebSocket");
    
    let (mut write, mut read) = ws_stream.split();

    let auth_json = serde_json::to_string(&auth).unwrap();
    if let Err(e) = write.send(auth_json.into()).await {
        eprintln!("❌ Ошибка отправки в WebSocket: {}", e);
        return;
    }
    
    eprintln!("📤 API ключ отправлен, ожидаем задание...");

    while let Some(msg) = read.next().await {
        match msg {
            Ok(msg) => {
                if msg.is_text() {
                    let text = msg.into_text().unwrap();
                    eprintln!("📥 Получено сообщение: {}", text);
                    
                    match serde_json::from_str::<Task>(&text) {
                        Ok(task) => {
                            println!("{}", serde_json::to_string(&task).unwrap());
                            eprintln!("✅ Задание успешно передано в stdout");
                            break;
                        }
                        Err(e) => {
                            eprintln!("❌ Ошибка парсинга задания: {}", e);
                        }
                    }
                }
            }
            Err(e) => {
                eprintln!("❌ Ошибка чтения из WebSocket: {}", e);
                break;
            }
        }
    }
    
    eprintln!("👋 Rust CLI завершает работу");
}
