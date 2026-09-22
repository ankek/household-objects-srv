<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<template>
  <section>
    <h1>Not found</h1>
    <p>That page does not exist. <RouterLink to="/items">Back to the inventory</RouterLink></p>
  </section>
</template>
