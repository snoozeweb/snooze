<template>
  <div>
    <APIElementField v-model="datavalue" endpoint="permissions" :colorize="colorize" :data="data"/>
  </div>
</template>

<script>
import Base from './Base.vue'
import APIElementField from './APIElementField.vue'

export default {
  extends: Base,
  components: {
    APIElementField,
  },
  props: ['value', 'options', 'colorize', 'data'],
  data() {
    return {
      datavalue: this.value,
    }
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('input', this.datavalue)
      },
      immediate: true
    },
  },
}
</script>
