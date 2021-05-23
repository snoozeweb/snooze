<template>
  <div>
    Command script:
    <APIElement v-model="datavalue" endpoint="command" primary="name"/>
  </div>
</template>

<script>
import Base from './Base.vue'
import APIElement from './APIElement.vue'

export default {
  extends: Base,
  components: {
    APIElement,
  },
  props: ['value', 'options'],
  data() {
    return {
      datavalue: this.value,
    }
  },
}
</script>
