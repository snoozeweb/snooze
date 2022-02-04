<template>
  <CFormInput
    :disabled=this.disabled
    :invalid=this.invalid
    :valid=this.valid
    :plainText=this.plainText
    @change=this.handleChange($event)
    @input=this.handleInput($event)
    :readonly=this.readonly
    :type=this.inputType
    v-model=this.dataValue
  >
  </CFormInput>
</template>

<script>
import { revParseNumber, parseNumber } from '@/utils/api'

export default {
  name: 'SFormInput',
  props: {
    /**
     * Toggle the disabled state for the component.
     */
    disabled: {
      type: Boolean,
      required: false,
    },
    /**
     * Set component validation state to invalid.
     */
    invalid: {
      type: Boolean,
      required: false,
    },
    /**
     * The default name for a value passed using v-model.
     */
    modelValue: {
      type: String,
      default: undefined,
      require: false,
    },
    /**
     * Render the component styled as plain text. Removes the default form field styling and preserve the correct margin and padding. Recommend to use only along side `readonly`.
     */
    plainText: {
      type: Boolean,
      required: false,
    },
    /**
     * Toggle the readonly state for the component.
     */
    readonly: {
      type: Boolean,
      required: false,
    },
    /**
     * Size the component small or large.
     *
     * @values 'sm' | 'lg'
     */
    size: {
      type: String,
      default: undefined,
      require: false,
    },
    /**
     * Specifies the type of component.
     *
     * @values 'color' | 'file' | 'text' | string
     */
    inputType: {
      type: String,
      default: 'text',
      require: false,
    },
    /**
     * Set component validation state to valid.
     */
    valid: {
      type: Boolean,
      required: false,
    },
  },
  emits: [
    /**
     * Event occurs when the element loses focus, after the content has been changed.
     */
    'change',
    /**
     * Event occurs immediately after the value of a component has changed.
     */
    'input',
    /**
     * Emit the new value whenever there’s an input or change event.
     */
    'update:modelValue',
  ],
  data () {
    return {
      dataValue: revParseNumber(this.modelValue),
      disabled: this.disabled,
      invalid: this.invalid,
      valid: this.valid,
      plainText: this.plainText,
      readonly: this.readonly,
      inputType: this.inputType,
    }
  },
  methods: {
    handleChange (event) {
      const target = event.target
      this.$emit('change', event)
      this.$emit('update:modelValue', parseNumber(target.value))
    },
    handleInput (event) {
      const target = event.target
      this.$emit('input', event)
      this.$emit('update:modelValue', parseNumber(target.value))
    },
  },
}
</script>
