<template>
  <div class="h-100">
    <CForm
      class="d-flex align-items-center mb-0 h-100"
    >
      <CFormCheck
        inline
        v-for="opts in options"
        @change="onChange"
        :checked="opts.value != undefined ? datavalue.weekdays.includes(opts.value) : datavalue.weekdays.includes(opts)"
        :value="opts.value != undefined ? opts.value : opts"
        :label="opts.text != undefined ? opts.text : opts"
      />
    </CForm>
  </div>
</template>

<script>
import Base from './Base.vue'

var default_object = {'weekdays': []}

export default {
  extends: Base,
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Object, default: () => Object.assign({}, default_object)},
  },
  data() {
    return {
      datavalue: {'weekdays': this.modelValue['weekdays'] || []},
      options: [
        {text: 'Monday',    value: 1},
        {text: 'Tuesday',   value: 2},
        {text: 'Wednesday', value: 3},
        {text: 'Thursday',  value: 4},
        {text: 'Friday',    value: 5},
        {text: 'Saturday',  value: 6},
        {text: 'Sunday',    value: 7},
      ],
    }
  },
  methods: {
    onChange(e) {
      var val = parseInt(e.target.value)
      var index = this.datavalue.weekdays.indexOf(val)
      if (e.target.checked && index == -1) {
        this.datavalue.weekdays.push(val)
      } else if(!e.target.checked && index >= 0) {
        this.datavalue.weekdays.splice(index)
      }
    }
  },
  watch: {
    datavalue: {
      handler() { this.$emit('update:modelValue', this.datavalue) },
      immediate: true,
      deep: true,
    },
  },
}
</script>
