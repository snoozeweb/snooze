<template>
  <div>
    <h5 v-if="Object.keys(datavalue).length === 0 && datavalue.constructor === Object"><CBadge color="primary">Forever</CBadge></h5>
    <CForm class="m-0" @submit.prevent>
      <template v-for="(constraint, ctype) in datavalue">
        <CRow v-for="(val, k) in constraint" :key="ctype+'_'+k" class="mb-2 g-0">
          <CCol xs="1">
            <CButton color="danger" size="lg" v-on:click="remove_component(ctype, k)" @click.stop.prevent>X</CButton>
          </CCol>
          <CCol xs="11" class="m-auto">
            <component
              :is="detect_constraint(ctype)"
              :id="'component_'+ctype+'_'+k"
              v-model="constraint[k]"
            />
          </CCol>
        </CRow>
      </template>
    </CForm>
    <CForm inline>
      <CRow class="g-0">
        <CCol xs="auto">
          <CInputGroup>
            <CFormSelect v-model="selected" class="col-form-label">
              <option v-for="opts in components" :value="opts.value">{{ opts.text }}</option>
            </CFormSelect>
            <CButton color="secondary" v-on:click="add_component(selected)" @click.stop.prevent>
              <i class="la la-plus la-lg"></i>
            </CButton>
          </CInputGroup>
        </CCol>
      </CRow>
    </CForm>
  </div>
</template>

<script>
import Base from './Base'

import DateTime from '@/components/form/DateTime'
import Time from '@/components/form/Time'
import Weekdays from '@/components/form/Weekdays'

export default {
  extends: Base,
  name: 'TimeConstraint',
  components: {
    DateTime,
    Time,
    Weekdays,
  },
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Object, default: () => {}},
  },
  data() {
    return {
      date_toggle: false,
      selected: 'datetime',
      components: [
        {value: 'datetime', text: 'DateTime'},
        {value: 'time', text: 'Time'},
        {value: 'weekdays', text: 'Weekdays'},
      ],
      datavalue: this.modelValue || {},
    }
  },
  methods: {
    add_component(component_type) {
      if (!(component_type in this.datavalue)) {
        this.datavalue[component_type] = []
      }
      this.datavalue[component_type].splice(this.datavalue[component_type].length, 1, {})
    },
    remove_component(component_type, component_index) {
      this.datavalue[component_type].splice(component_index, 1)
      if (this.datavalue[component_type].length == 0) {
        delete this.datavalue[component_type]
      }
    },
    detect_constraint(constraint_name) {
      switch (constraint_name) {
        case 'datetime':
          return 'DateTime'
        case 'time':
          return 'Time'
        case 'weekdays':
        default:
          return 'Weekdays'
      }
    },
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
