<template>
<span>
  <span v-for="action,k in data" :key="k">
    <span v-if="k != 0">, </span>
    <span v-if="action[0] == 'DELETE'">
    <b>{{ action[0] }}</b> {{ action[1] }}</span>
    <span v-else>
    <b>{{ action[0] }}</b> {{ action[1] }} = {{ action[2] }}</span>
  </span>
</span>
</template>

<script>

export default {
  name: 'Action',
  props: {
    data: {type: [Array, String]},
  },
}
</script>
