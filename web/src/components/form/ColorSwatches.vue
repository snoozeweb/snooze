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
import VSwatches from 'vue3-swatches'
import { gen_palette } from '@/utils/colors'

export default {
  extends: Base,
  components: {
    VSwatches,
  },
  emits: ['update:modelValue'],
  props: ['modelValue'],
  data() {
    return {
      datavalue: this.modelValue,
      swatches: [],
    }
  },
  mounted() {
    this.swatches = gen_palette(8, 8)
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('update:modelValue', this.datavalue)
      },
      immediate: true
    },
  },
}
</script>
