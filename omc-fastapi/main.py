import asyncio
import logging
import time
from contextlib import asynccontextmanager

from fastapi import FastAPI, WebSocket, WebSocketDisconnect, WebSocketException
from fastapi.middleware.cors import CORSMiddleware

LOGGER_FORMAT = "%(asctime)s - %(levelname)s - %(name)s - %(message)s"

logging.basicConfig(format=LOGGER_FORMAT)

logger = logging.getLogger(__name__)
logger.name
logger.setLevel(logging.INFO)

@asynccontextmanager
async def lifespan(_: FastAPI):
    """Lifespan context manager for FastAPI app"""
    # Startup: Create the background task for message cleanup
    cleanup_task = asyncio.create_task(remove_old_messages())
    yield
    # Shutdown: Cancel the background task
    cleanup_task.cancel()
    try:
        await cleanup_task
    except asyncio.CancelledError:
        pass

app = FastAPI(lifespan=lifespan)
clients: list[WebSocket] = []


class Message:
    """Message structure"""
    message: str
    timestamp: float

    def __init__(self, message: str, timestamp: float):
        self.message = message
        self.timestamp = timestamp


# Format: [("message", timestamp)]
messages: list[Message] = []

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

MESSAGE_DISAPPEAR_THRESHOLD = 60  # Seconds


async def remove_old_messages():
    """Remove old messages in background"""
    while True:
        now = time.time()
        global messages
        bef_messages = len(messages)
        messages = [msg for msg in messages if now - msg.timestamp < MESSAGE_DISAPPEAR_THRESHOLD]
        aft_messages = len(messages)
        logger.info("Removed %i messages", bef_messages - aft_messages)
        logger.info("Total messages: %i", aft_messages)
        await asyncio.sleep(5)


@app.get("/messages")
async def get_messages():
    """Return all messages"""
    return {"messages": [{"message": msg.message, "timestamp": msg.timestamp} for msg in messages]}


@app.websocket("/ws")
async def websocket_endpoint(websocket: WebSocket):
    """Handle the WebSocket endpoint and contract"""
    await websocket.accept()
    logger.info("Connected to websocket %s", websocket)
    clients.append(websocket)

    try:
        while True:
            data = await websocket.receive_text()
            logger.info("Received message: %s", data)
            messages.append(Message(data, time.time()))

            for client in clients:
                await client.send_text(data)
    except WebSocketException:
        msg = "Error sendi message"
        logger.exception(msg)
    except WebSocketDisconnect:
        clients.remove(websocket)
