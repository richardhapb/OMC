use base64::Engine;
use sha1::Digest;
use std::sync::Arc;
use tokio::io::{AsyncBufReadExt, AsyncReadExt, AsyncWriteExt, BufReader, BufWriter};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::mpsc::{Receiver, Sender};
use tokio::sync::{mpsc, Mutex};

type WsResult = Result<(), Box<dyn std::error::Error + Send + Sync>>;
type ChatResult = Result<(), Box<dyn std::error::Error + Send + Sync>>;

const MAGIC_UUID: &str = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

enum ChatEvent {
    NewMessage(Message),
    ClientJoined(Arc<WebSocket>),
    ClientLeft(Arc<WebSocket>),
}

struct WebSocket {
    socket: Mutex<TcpStream>,
    connected: bool,
}

struct Message {
    text: String,
    timestamp: i64,
}

struct Chat {
    messages: Vec<Message>,
    mutex: Mutex<Vec<Message>>,
    tx: Sender<ChatEvent>,
}

struct Participants {
    websockets: Vec<Arc<WebSocket>>,
    mutex: Mutex<Vec<Arc<WebSocket>>>,
}

impl Default for Participants {
    fn default() -> Self {
        Self {
            websockets: Vec::new(),
            mutex: Mutex::new(Vec::new()),
        }
    }
}

impl Default for Chat {
    fn default() -> Self {
        let (tx, _) = mpsc::channel(100);
        Self {
            messages: Vec::new(),
            mutex: Mutex::new(Vec::new()),
            tx,
        }
    }
}

#[tokio::main]
async fn main() -> WsResult {
    let listener = TcpListener::bind("0.0.0.0:8000").await?;

    println!("Listening on 0.0.0.0:8000");

    let (tx, rx) = mpsc::channel::<ChatEvent>(100);

    let participants = Arc::new(Participants::default());
    let chat = Arc::new(Chat {
        messages: Vec::new(),
        mutex: Mutex::new(Vec::new()),
        tx,
    });

    let participants_handler = participants.clone();
    let chat_handler = chat.clone();
    tokio::spawn(handle_chat_events(rx, participants_handler, chat_handler));

    loop {
        let (socket, addr) = listener.accept().await?;
        println!("Client connected: {}", addr);

        let chat_handler = chat.clone();
        tokio::spawn(async move {
            // Changed the return type of handle_connection to ChatResult
            if let Err(e) = handle_connection(socket, chat_handler).await {
                println!("Error in connection: {}, {}", addr, e);
            }
        });
    }
}

async fn handle_chat_events(
    mut rx: Receiver<ChatEvent>,
    participants: Arc<Participants>,
    chat: Arc<Chat>,
) -> ChatResult {
    while let Some(event) = rx.recv().await {
        match event {
            ChatEvent::NewMessage(message) => {
                println!("Message received: {}", message.text);
                {
                    let mut chat_guard = chat.mutex.lock().await;
                    chat_guard.push(message);
                }

                let participants_guard = participants.mutex.lock().await;
                for participant in participants_guard.iter() {
                    if participant.connected {
                        // TODO: Implement send_message_to_client correctly
                        // by acquiring the Mutex lock on participant.socket
                        // and then using `write_all`.
                        // For now, keeping todo!()
                        // send_message_to_client(&participant.socket, &message).await?;
                        todo!()
                    }
                }
            }
            ChatEvent::ClientJoined(ws) => {
                let mut participants_guard = participants.mutex.lock().await;
                participants_guard.push(ws.clone());
                println!(
                    "Client joined to chat: {:?}",
                    ws.socket.lock().await.local_addr()
                );
            }
            ChatEvent::ClientLeft(ws) => {
                let mut participants_guard = participants.mutex.lock().await;
                participants_guard.retain(|participant| !Arc::ptr_eq(participant, &ws));
                println!(
                    "Client left the chat: {:?}",
                    ws.socket.lock().await.local_addr()
                );
            }
        }
    }

    Ok(())
}

async fn send_message_to_client(socket: &Mutex<TcpStream>, message: &str) -> ChatResult {
    let mut stream = socket.lock().await;
    // TODO: Implement actual WebSocket frame sending here
    // For example: stream.write_all(message.as_bytes()).await?;
    todo!()
}

async fn handle_connection(mut socket: TcpStream, chat: Arc<Chat>) -> ChatResult {
    let mut buf: Vec<u8> = vec![0x0; 1024];

    match socket.read(&mut buf[..]).await {
        Ok(n) => {
            if n == 0 {
                return Ok(()); // Connection closed
            } else {
                println!("Received {} bytes from client", n);
                handle_request(&buf, socket, chat)
                    .await
                    .unwrap_or_else(|e| println!("Error handling request: {}", e)); // This `e` is now `Send`
                n
            }
        }
        Err(e) => Err(format!("Error reading from tcp: {}", e))?,
    };

    Ok(())
}

async fn handle_request(buf: &[u8], socket: TcpStream, chat: Arc<Chat>) -> WsResult {
    let mut reader = BufReader::new(buf);
    let mut pre = String::new();
    let n = reader.read_line(&mut pre).await?;
    if n == 0 {
        return Ok(()); // Connection closed
    }

    let parts: Vec<&str> = pre.split(' ').map(|p| p.trim()).collect();
    let method = parts[0];
    let uri = parts[1];
    println!("Received method: {}, URI: {}", method, uri);

    if method == "GET" && uri == "/ws" {
        println!("Websocket request received");
        handle_websocket(buf, socket, chat).await?;
        return Ok(());
    }

    if method == "GET" && uri == "/messages" {
        todo!()
    }

    Ok(())
}

async fn handle_websocket(buf: &[u8], mut socket: TcpStream, chat: Arc<Chat>) -> WsResult {
    let reader = BufReader::new(buf);

    {
        let mut writer = BufWriter::new(&mut socket);

        let mut lines = reader.lines();
        let mut key = "".into();
        while let Ok(Some(line)) = lines.next_line().await {
            if line.contains("Sec-WebSocket-Key:") {
                let parts = line.splitn(2, ':').map(|p| p.trim()).collect::<Vec<&str>>();
                key = parts[1].to_string();
                break;
            }
        }

        if key.is_empty() {
            return Err("No WebSocket key found".into());
        }

        let key_result = calculate_accepted_key(key);

        let response = format!(
            "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: {}\r\n\r\n",
            key_result
        );

        println!("\nResponse to WebSocket: {}", response);

        writer.write_all(response.as_bytes()).await?;
    }

    ws_loop(socket, chat)
        .await
        .unwrap_or_else(|e| println!("Error in websocket: {}", e));

    Ok(())
}

async fn ws_loop(socket: TcpStream, chat: Arc<Chat>) -> WsResult {
    let new_participant = WebSocket {
        socket: Mutex::new(socket),
        connected: true,
    };

    let ws_arc = Arc::new(new_participant);

    chat.tx
        .send(ChatEvent::ClientJoined(ws_arc.clone()))
        .await?;

    loop {
        match read_frame(&ws_arc.socket).await {
            Ok(Some(message)) => {
                chat.tx
                    .send(ChatEvent::NewMessage(Message {
                        text: message,
                        timestamp: chrono::Utc::now().timestamp_millis(),
                    }))
                    .await?;
            }
            Ok(None) => {}
            Err(e) => {
                println!("Connection error: {}", e);
                break;
            }
        }
    }

    chat.tx.send(ChatEvent::ClientLeft(ws_arc.clone())).await?;

    Ok(())
}

async fn read_frame(
    socket: &Mutex<TcpStream>,
) -> Result<Option<String>, Box<dyn std::error::Error + Send + Sync>> {
    let mut stream = socket.lock().await;

    let mut header_buf = [0u8; 2];
    // Check if read_exact actually reads 2 bytes or if the connection closed
    let bytes_read = stream.read_exact(&mut header_buf).await?;
    if bytes_read == 0 { // This case handles EOF right at the start of a frame
        return Ok(None);
    }
    if bytes_read < 2 {
        // This case should ideally not happen with read_exact, but good to be robust
        return Err("Incomplete WebSocket frame header".into());
    }

    let _fin = (header_buf[0] & 0b10000000) != 0;
    let opcode = header_buf[0] & 0b00001111;
    let masked = (header_buf[1] & 0b10000000) != 0;
    let mut payload_len = (header_buf[1] & 0b01111111) as usize;

    if payload_len == 126 {
        let mut extended = [0u8; 2];
        stream.read_exact(&mut extended).await?;
        payload_len = ((extended[0] as usize) << 8) | (extended[1] as usize);
    } else if payload_len == 127 {
        let mut extended = [0u8; 8];
        stream.read_exact(&mut extended).await?;
        payload_len = ((extended[0] as usize) << 56)
            | ((extended[1] as usize) << 48)
            | ((extended[2] as usize) << 40)
            | ((extended[3] as usize) << 32)
            | ((extended[4] as usize) << 24)
            | ((extended[5] as usize) << 16)
            | ((extended[6] as usize) << 8)
            | (extended[7] as usize);
    }

    match opcode {
        0x1 => {
            let mut payload = vec![0; payload_len];
            stream.read_exact(&mut payload).await?;

            if masked {
                let mut mask_bytes = [0u8; 4];
                stream.read_exact(&mut mask_bytes).await?;
                for i in 0..payload_len {
                    payload[i] ^= mask_bytes[i % 4];
                }
            }

            Ok(Some(String::from_utf8_lossy(&payload).to_string()))
        }
        0x8 => {
            // Read any remaining payload for the close frame to ensure stream is clear
            let mut _discard_payload = vec![0; payload_len];
            stream.read_exact(&mut _discard_payload).await?;
            if masked {
                let mut _discard_mask = [0u8; 4];
                stream.read_exact(&mut _discard_mask).await?;
            }
            Err("WebSocket connection closed".into())
        }
        _ => {
            // Discard payload for unknown frame types
            let mut _discard_payload = vec![0; payload_len];
            stream.read_exact(&mut _discard_payload).await?;
            if masked {
                let mut _discard_mask = [0u8; 4];
                stream.read_exact(&mut _discard_mask).await?;
            }
            Ok(None)
        }
    }
}

fn calculate_accepted_key(key: String) -> String {
    let mut h = sha1::Sha1::new();
    h.update(format!("{}{}", key, MAGIC_UUID));

    base64::prelude::BASE64_STANDARD.encode(h.finalize())
}
