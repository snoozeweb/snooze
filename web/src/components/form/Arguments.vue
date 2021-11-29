<template>
  <div>
    <CForm @submit.prevent>
      <CRow class="g-0" v-for="(argument, index) in this.datavalue" :key="index">
        <CCol xs="auto">
          <CInputGroup class="pb-1">
            <CFormInput v-model=argument[0] :placeholder="placeholder[0]" class="col-form-label"/>
            <CFormInput v-model=argument[1] :placeholder="placeholder[1]" class="col-form-label"/>
            <CTooltip content="Remove">
              <template #toggler="{ on }">
                <CButton @click="remove(index)" @click.stop.prevent color="danger" v-on="on">
                  <i class="la la-trash la-lg"></i>
                </CButton>
              </template>
            </CTooltip>
          </CInputGroup>
        </CCol>
      </CRow>
      <CRow class="g-0">
        <CCol xs="auto">
          <CTooltip content="Add">
            <template #toggler="{ on }">
              <CButton @click="append" @click.stop.prevent color="secondary" v-on="on"><i class="la la-plus la-lg"></i></CButton>
            </template>
          </CTooltip>
        </CCol>
      </CRow>
    </CForm>
  </div>
</template>

<script>
import Base from './Base.vue'

export default {
  extends: Base,
  name: 'Arguments',
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Array, default: () => []},
    options: {},
    placeholder: {type: Array, default: () => ['--key', 'value']},
  },
  data () {
    return {
      datavalue: this.modelValue,
      add_key: null,
      add_value: null,
    }
  },
  methods: {
    append () {
      this.datavalue.push(['', ''])
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
      immediate: true,
    },
  },
}
</script>
