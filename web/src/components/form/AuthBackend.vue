<template>
  <div>
    <APIElement v-model="datavalue" endpoint="login" primary="endpoint" subkey="backends"/>
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
