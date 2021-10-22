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
import { gen_palette } from '@/utils/colors'

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
    this.swatches = gen_palette(8, 8)
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
