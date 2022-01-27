<template>

<span>
  <span v-if="datavalue[0] == 'OR' || datavalue[0] == 'AND'">
    <CForm @submit.prevent class="row g-0">
      <CCol xs="auto">
        <CInputGroup>
          <CFormSelect v-model="datavalue[0]" :value="datavalue[0]">
            <option v-for="opts in logic" :value="opts.value">{{ opts.text }}</option>
          </CFormSelect>
          <CTooltip content="Remove" trigger="hover">
            <template #toggler="{ on }">
              <CButton v-c-tooltip="{content: 'Remove'}" @click="datavalue = datavalue[1]" @click.stop.prevent color="danger" v-on="on">
                <i class="la la-trash la-lg"></i>
              </CButton>
            </template>
          </CTooltip>
        </CInputGroup>
      </CCol>
    </CForm>
    <ul>
      <CForm @submit.prevent class="pt-1">
        <Condition v-model="datavalue[1]" :parent_value="datavalue[2]">
          <template #parent_comp>
            <CTooltip content="Remove" trigger="hover">
              <template #toggler="{ on }">
                <CButton v-if="is_not_operation(datavalue[1])" @click="datavalue = datavalue[2]" @click.stop.prevent color="danger" v-on="on"><i class="la la-trash la-lg"></i></CButton>
              </template>
            </CTooltip>
          </template>
        </Condition>
      </CForm>
      <CForm @submit.prevent class="pt-1">
        <Condition v-model="datavalue[2]" :parent_value="datavalue[1]">
          <template #parent_comp>
            <CTooltip content="Remove" trigger="hover">
              <template #toggler="{ on }">
                <CButton v-if="is_not_operation(datavalue[2])" @click="datavalue = datavalue[1]" @click.stop.prevent color="danger" v-on="on"><i class="la la-trash la-lg"></i></CButton>
              </template>
            </CTooltip>
          </template>
        </Condition>
      </CForm>
    </ul>
  </span>
  <span v-else-if="datavalue[0] == 'NOT'">
    <CForm @submit.prevent class="row g-0">
      <CCol xs="auto">
        <CInputGroup>
          <CFormSelect v-model="datavalue[0]" :value="datavalue[0]">
            <option v-for="opts in logic" :value="opts.value">{{ opts.text }}</option>
          </CFormSelect>
          <CButton @click="datavalue = datavalue[1]" @click.stop.prevent color="danger">
            <i class="la la-trash la-lg"></i>
          </CButton>
        </CInputGroup>
      </CCol>
    </CForm>
    <ul>
      <CForm @submit.prevent class="pt-1">
        <Condition v-model="datavalue[1]" :parent_value="datavalue[1]">
          <template #parent_comp>
            <CTooltip content="Remove" trigger="hover">
              <template #toggler="{ on }">
                <CButton  v-if="is_not_operation(datavalue[1])" @click="datavalue = datavalue[1]" @click.stop.prevent color="danger" v-on="on"><i class="la la-trash la-lg"></i></CButton>
              </template>
            </CTooltip>
          </template>
        </Condition>
      </CForm>
    </ul>
  </span>
  <span v-else>
    <CForm @submit.prevent>
      <CInputGroup>
        <CFormInput v-model="datavalue[1]" style="flex: 0 0 auto; width: 25%"/>
        <CFormSelect v-model="datavalue[0]" :value="datavalue[0]" style="flex: 0 0 auto; width: 15%">
          <option v-for="opts in operations" :value="opts.value">{{ opts.text }}</option>
        </CFormSelect>
        <CFormInput v-model="datavalue[2]" v-if="datavalue[0] != 'EXISTS' && datavalue[0] != 'SEARCH'" />
        <CTooltip content="Add" trigger="hover">
          <template #toggler="{ on }">
            <CButton @click="datavalue = ['OR', datavalue, ['']]" @click.stop.prevent color="secondary" v-on="on">
              <i class="la la-plus la-lg"></i>
            </CButton>
          </template>
        </CTooltip>
        <CTooltip content="Reset" trigger="hover">
          <template #toggler="{ on }">
            <CButton @click="datavalue = [''];" @click.stop.prevent color="info" v-on="on">
              <i class="la la-redo-alt la-lg"></i>
            </CButton>
          </template>
        </CTooltip>
        <slot name="parent_comp"/>
      </CInputGroup>
    </CForm>
  </span>
</span>

</template>

<script>

import Base from './Base.vue'

export default {
  extends: Base,
  name: 'Condition',
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Array, default: () => ['']},
    parent_value: {type: Array, default: () => ['']},
    options: {},
  },
  data () {
    return {
      datavalue: this.modelValue,
      operations: [
        {value: '=', text: '='},
        {value: '!=', text: '!='},
        {value: '>', text: '>'},
        {value: '>=', text: '>='},
        {value: '<', text: '<'},
        {value: '<=', text: '<='},
        {value: 'MATCHES', text: 'matches'},
        {value: 'EXISTS', text: 'exists?'},
        {value: 'CONTAINS', text: 'contains'},
        {value: 'SEARCH', text: 'search'},
      ],
      logic: [
        {value: 'OR', text: 'OR'},
        {value: 'AND', text: 'AND'},
        {value: 'NOT', text: 'NOT'},
      ],
    }
  },
  methods: {
    is_not_operation (v) {
      return v[0] !== 'OR' && v[0] !== 'AND' && v[0] !== 'NOT'
    }
  },
  computed: {
    operation: {
      get () { return this.operations[this.datavalue[0]] || this.logic[this.datavalue[0]] },
      set (op) { this.datavalue[0] = op }
    },
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
