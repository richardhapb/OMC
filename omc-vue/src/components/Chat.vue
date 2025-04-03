<template>
    <div class="chat-container">
        <div class="messages">
            <div v-for="(msg, index) in messages" :key="index" class="message">
                {{ msg }}
            </div>
        </div>
        <input v-model="newMessage" @keyup.enter="sendMessage" placeholder="Type a message..." autofocus />
    </div>
</template>

<script>
export default {
    data() {
        return {
            ws: null,
            messages: [],
            newMessage: ""
        };
    },
    methods: {
        sendMessage() {
            if (this.newMessage.trim()) {
                this.ws.send(this.newMessage);
                this.newMessage = "";
            }
        },
        async initializeMessages () {
            try {
                const response = await fetch("http://127.0.0.1:8000/messages", {
                    method: "GET",
                    headers: {
                        'Accept': 'application/json'
                    }
                });

                if (!response.ok) {
                    throw new Error(response.error);
                }

                const data = await response.json();
                
                // Remove messages after timeout returned from server
                data.messages.forEach(msg => {
                    const timestamp = msg.timestamp;
                    // 1000 = Tranform to milliseconds and 60000 = 60 seconds
                    const timeout = 60000 - (Number.parseFloat(Date.now()) - timestamp * 1000);
                    console.log(timeout);

                    if (timeout > 0) {
                        this.messages.push(msg.message);

                        setTimeout(() => {
                            const index = this.messages.indexOf(msg.message);
                            if (index > -1) {
                                this.messages.splice(index, 1);
                            }
                        }, timeout);
                    }
                });

            } catch (error) {
                console.error(error);
            }

        }
    },
    mounted() {
        this.ws = new WebSocket("ws://localhost:8000/ws");
        this.ws.onerror = (error) => {
            console.error("WebSocket error:", error);
        };

        this.ws.onmessage = (event) => {
            this.messages.push(event.data);
            setTimeout(() => {
                this.messages.shift();
            }, 60000);  // 60 seconds
        }

        this.initializeMessages();
    },
    beforeUnmount() {
        this.ws.close();
    }
};
</script>

<style scoped>
input {
    min-height: 30px;
    width: 100%;
    margin: 0.5rem auto;
}

.chat_container {
    width: 300px;
    min-height: 500px;
    margin: auto;
    text-align: center;
}

.messages {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    background-color: #333;
    min-height: 500px;
    padding: 5px;
    margin: 5px 0;
    border-radius: 5px;
    border: 1pt solid white;
}

.message {
    color: white;
    background-color: #555;
    padding: 0.2rem;
    font-size: 1.2rem;
}
</style>

