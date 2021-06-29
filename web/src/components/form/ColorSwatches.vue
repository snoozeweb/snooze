<template>
  <div>
    <v-swatches
      v-model="datavalue"
      :swatches="swatches"
      show-fallback
      fallback-input-type="color"
      popover-x="left"
    ></v-swatches>
  </div>
</template>

<script>
import Base from './Base.vue'
import VSwatches from 'vue-swatches'

export default {
  extends: Base,
  components: {
    VSwatches,
  },
  props: ['value'],
  data() {
    return {
      datavalue: this.value,
      swatches: [],
    }
  },
  mounted() {
    var swatches = []
    var bodystyle = window.getComputedStyle(document.body)
    swatches.push(bodystyle.getPropertyValue('--primary').trim())
    swatches.push(bodystyle.getPropertyValue('--secondary').trim())
    swatches.push(bodystyle.getPropertyValue('--success').trim())
    swatches.push(bodystyle.getPropertyValue('--info').trim())
    swatches.push(bodystyle.getPropertyValue('--warning').trim())
    swatches.push(bodystyle.getPropertyValue('--danger').trim())
    swatches.push(bodystyle.getPropertyValue('--light').trim())
    swatches.push(bodystyle.getPropertyValue('--dark').trim())
    this.swatches = swatches
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
