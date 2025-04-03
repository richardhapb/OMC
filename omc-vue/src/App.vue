<script setup>
import Chat from '@/components/Chat.vue'
</script>

<template>
    <div class="container">
        <header>
            <h1>One minute chat</h1>
        </header>

        <main>
            <Chat />
        </main>
    </div>
</template>

<style scoped>
h1 {
    font-size: 3rem;
    text-align: center;
    margin-bottom: 3rem;
}

.container {
    display: flex;
    flex-direction: column;
}
</style>
