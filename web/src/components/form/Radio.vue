<template>
  <div>
    <CForm class="m-0">
      <CButtonGroup role="group">
        <CFormCheck
          type="radio"
          autocomplete="off"
          :name="id"
          :button="{color: 'primary', variant: 'outline'}"
          v-for="(opts, i) in options"
          :id="id + i"
          @click="opts.value != undefined ? (datavalue = opts.value) : (datavalue = opts)"
          :checked="opts.value != undefined ? (opts.value == datavalue) : (opts == datavalue)"
          :value="opts.value != undefined ? opts.value : opts"
          :label="opts.text != undefined ? opts.text : opts"
        />
      </CButtonGroup>
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
      type: Array,
    },
    default_value: {
      type: [Object, String, Number, Boolean],
    },
  },
  data() {
    return {
      datavalue: [undefined, '', [], {}].includes(this.modelValue) ? (this.default_value == undefined ? '' : this.default_value) : this.modelValue
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
