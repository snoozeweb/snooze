<template>
  <div>
    <CForm inline>
    <CInputGroup class='pe-1'>
      <CInputGroupText>Red</CInputGroupText>
      <CFormSelect v-model="datavalue.red" :value="datavalue.red">
        <option v-for="opts in light_options" :value="opts">{{ opts }}</option>
      </CFormSelect>
    </CInputGroup>
    <CInputGroup class='pe-1'>
      <CInputGroupText>Yellow</CInputGroupText>
      <CFormSelect v-model="datavalue.yellow" :value="datavalue.yellow">
        <option v-for="opts in light_options" :value="opts">{{ opts }}</option>
      </CFormSelect>
    </CInputGroup>
    <CInputGroup class='pe-1'>
      <CInputGroupText>Green</CInputGroupText>
      <CFormSelect v-model="datavalue.green" :value="datavalue.green">
        <option v-for="opts in light_options" :value="opts">{{ opts }}</option>
      </CFormSelect>
    </CInputGroup>
    <CInputGroup class='pe-1'>
      <CInputGroupText>Blue</CInputGroupText>
      <CFormSelect v-model="datavalue.blue" :value="datavalue.blue">
        <option v-for="opts in light_options" :value="opts">{{ opts }}</option>
      </CFormSelect>
    </CInputGroup>
    <CInputGroup>
      <CInputGroupText>White</CInputGroupText>
      <CFormSelect v-model="datavalue.white" :value="datavalue.white">
        <option v-for="opts in light_options" :value="opts">{{ opts }}</option>
      </CFormSelect>
    </CInputGroup>
    </CForm>
  </div>
</template>

<script>

import Base from './Base.vue'

var default_patlite = {red: 'off', yellow: 'off', green: 'off', blue: 'off', white: 'off'}

export default {
  extends: Base,
  name: 'Patlite',
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Object, default: () => Object.assign({}, default_patlite) },
    options: {type: Object},
  },
  data() {
    return {
      light_options: ['off', 'on', 'blink1', 'blink2'],
      datavalue: this.modelValue,
    }
  },
  watch: {
    datavalue: {
      handler(v) { this.$emit('update:modelValue', this.datavalue) },
      deep: true,
      immediate: true
    },
  },
}

</script>
