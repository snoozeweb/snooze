<template>

  <div>
    <CForm  @submit.prevent>
      <CRow class="g-0" v-for="(val, index) in this.datavalue" :key="index">
        <CCol xs="auto">
          <CInputGroup class="pb-1">
            <CFormSelect v-model="val[0]" :value="val[0]" style="width: auto">
              <option v-for="opts in operations" :value="opts.value">{{ opts.text }}</option>
            </CFormSelect>
            <CFormInput v-model="val[1]"/>
            <CFormInput v-model="val[2]" v-if="val[0] != 'DELETE'"/>
            <CButton v-on:click="remove(index)" @click.stop.prevent color="danger">
              <i class="la la-trash la-lg"></i>
            </CButton>
          </CInputGroup>
        </CCol>
      </CRow>
    </CForm>
    <CCol xs="auto">
      <CTooltip content="Add">
        <template #toggler="{ on }">
          <CButton @click="append" @click.stop.prevent color="secondary" v-on="on"><i class="la la-plus la-lg"></i></CButton>
        </template>
      </CTooltip>
    </CCol>
  </div>

</template>

<script>

import Base from './Base.vue'

export default {
  extends: Base,
  name: 'Modification',
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Array, default: () => []},
    options: {},
  },
  data () {
    return {
      datavalue: this.modelValue,
      operations: [
        {value: 'SET', text: 'Set'},
        {value: 'DELETE', text: 'Delete'},
        {value: 'ARRAY_APPEND', text: 'Append (to array)'},
        {value: 'ARRAY_DELETE', text: 'Delete (from array)'},
        {value: 'REGEX_PARSE', text: 'Regex capture group'},
      ],
    }
  },
  methods: {
    append () {
      this.datavalue.push(['SET', '', ''])
      //this.add_key = null
      //this.add_value = null
    },
    remove (index) {
      this.datavalue.splice(index, 1)
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
