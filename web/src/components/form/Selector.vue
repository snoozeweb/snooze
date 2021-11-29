<template>
  <div>
    <CFormSelect @change="onChange" :value="datavalue" :required="required" :class="{ 'is-invalid': required && !checkField, 'is-valid': required && checkField }">
      <option v-if="!default_value && default_value != ''" disabled value="" :selected="datavalue == ''">Please select an option</option>
      <option v-for="opts in options" :value="opts.value || opts">{{ opts.text || opts }}</option>
    </CFormSelect>
    <CFormFeedback invalid>
      Field is required
    </CFormFeedback>
  </div>
</template>

<script>

import Base from './Base.vue'

// Create a selector form
export default {
  extends: Base,
  emits: ['update:modelValue'],
  props: {
    modelValue: {
      type: [String, Number, Boolean],
    },
    // Object containing the `{value: display_name}` of the
    // options of the selector
    options: {
      type: Array,
    },
    default_value: {
      type: [String, Number, Boolean],
    },
    required: {
      type: Boolean,
      default: () => false
    },
  },
  data() {
    return {
      datavalue: [undefined, '', [], {}].includes(this.modelValue) ? (this.default_value == undefined ? '' : this.default_value) : this.modelValue
    }
  },
  methods: {
    onChange(e) {
      this.datavalue = this.options[e.target.selectedIndex].value || this.options[e.target.selectedIndex]
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
  computed: {
    checkField () {
      return this.datavalue != ''
    }
  },
}

</script>
