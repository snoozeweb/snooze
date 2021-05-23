<template>
  <div>
    <b-form-textarea name="string" v-model="datavalue" />
  </div>
</template>

<script>

import Base from './Base.vue'

export default {
  extends: Base,
  props: ['value', 'options'],
  data() {
    return {
      datavalue: this.value
    }
  },
  watch: {
    datavalue () {
      this.$emit('input', this.datavalue)
    }
  },
}

</script>
