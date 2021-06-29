<template>
  <div>
    <APIElementField v-model="datavalue" endpoint="role" primary="name" :colorize="colorize" :data="data" :import_keys="import_keys"/>
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
  props: ['value', 'options', 'colorize', 'import_keys', 'data'],
  data() {
    return {
      datavalue: this.value,
    }
  },
  computed: {
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
