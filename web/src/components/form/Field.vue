<template>
<div>
  <SFormTags
    v-model="datavalue"
    placeholder="Enter fields separated by space"
    trim
  ></SFormTags>
</div>
</template>

<script>

import Base from './Base.vue'
import SFormTags from '@/components/SFormTags.vue'

export default {
  extends: Base,
  name: 'Condition',
  components: {
    SFormTags,
  },
  emits: ['update:modelValue'],
  props: {
    modelValue: {
      type: Array,
    },
    options: {
      type: Array,
    },
    default_value: {
      type: Array,
    },
  },
  data () {
    return {
      datavalue: [undefined, '', [], {}].includes(this.modelValue) ? (this.default_value == undefined ? [] : this.default_value) : this.modelValue,
      validTags: [],
      invalidTags: [],
      duplicateTags: [],
    }
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
