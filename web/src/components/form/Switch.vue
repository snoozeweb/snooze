<template>
  <div>
    <CForm class="m-0">
      <CFormSwitch
        class="pointer"
        @click="datavalue = !datavalue"
        :checked="datavalue"
        :size="options.size != undefined ? options.size : 'xl'"
        :label="options.text != undefined ? options.text : ''"
      />
    </CForm>
  </div>
</template>

<script>

import Base from './Base.vue'

// Create a selector form
export default {
  extends: Base,
  emits: ['update:modelValue'],
  props: {
    id: {
      type: String,
    },
    modelValue: {
      type: [Object, String, Number, Boolean],
    },
    // Object containing the `{value: display_name}` of the
    // options of the selector
    options: {
      type: Object,
      default: () => Object.assign({}, {size: 'xl', text: ''}),
    },
    default_value: {
      type: [Object, String, Number, Boolean],
    },
  },
  data() {
    return {
      datavalue: Boolean([undefined, '', [], {}].includes(this.modelValue) ? (this.default_value == undefined ? false : this.default_value) : this.modelValue)
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
