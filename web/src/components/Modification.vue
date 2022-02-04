<template>
<span>
  <span v-for="modification,k in data" :key="k">
    <span v-if="k != 0">, </span>
    <span v-if="modification[0] == 'DELETE'">
    <b>{{ modification[0] }}</b> {{ modification[1] }}</span>
    <span v-if="modification[0] == 'KV_SET'">
    <b>{{ modification[0] }}</b> {{ modification[3] }} = {{ modification[1] }}[{{ modification[2] }}]</span>
    <span v-else>
    <b>{{ modification[0] }}</b> {{ modification[1] }} = {{ JSON.stringify(modification[2]) }}</span>
  </span>
</span>
</template>

<script>

export default {
  name: 'Modification',
  props: {
    data: {type: [Array, String]},
  },
}
</script>
