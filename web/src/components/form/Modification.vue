<template>
  <div>
    <CForm  @submit.prevent>
      <CRow v-for="(val, index) in this.dataValue" :key="index" class="m-0 pb-2">
        <CCol class="p-0">
          <CInputGroup>
          <CFormSelect v-model="val[0]" :value="val[0]">
            <option v-for="opts in operations" v-bind:key="opts.value" :value="opts.value">{{ opts.text }}</option>
          </CFormSelect>
          <template v-if="val[0] == 'DELETE'">
            <CFormInput id="field" v-model="val[1]" placeholder="Field to delete" />
          </template>
          <template v-else-if="val[0] == 'REGEX_PARSE'">
            <CFormInput id="field" v-model="val[1]" placeholder="Field to parse" />
          </template>
          <template v-else-if="val[0] == 'REGEX_SUB'">
            <CFormInput id="field" v-model="val[1]" placeholder="Field to parse" />
            <CFormInput id="out_field" v-model="val[2]" placeholder="Output field" />
          </template>
          <template v-else-if="val[0] == 'KV_SET'">
            <CFormInput id="dict" v-model="val[1]" placeholder="Dictionary"/>
            <CFormInput id="field" v-model="val[2]" placeholder="Field"/>
            <CFormInput id="out_field" v-model="val[3]" placeholder="Output field"/>
          </template>
          <template v-else>
            <CFormInput id="field" v-model="val[1]" placeholder="Field"/>
            <SFormInput id="value" v-model="val[2]" placeholder="Value"/>
          </template>
          <CButton class="ms-auto" size="sm" color="secondary" v-c-tooltip="{content: documentation[val[0]], trigger: 'click', placement: 'bottom'}">
            <i class="la la-info la-lg"></i>
          </CButton>
          <CButton
            size="sm"
            color="danger"
            v-on:click="remove(index)"
            @click.stop.prevent
          >
            <i class="la la-trash la-lg"></i>
          </CButton>
          </CInputGroup>
          <CInputGroup v-if="val[0] == 'REGEX_PARSE'">
            <CFormTextarea class="" id="regex" v-model="val[2]" placeholder="Regex with capture groups (?P<field_name>.*?)" />
            <!--<CodeTextarea v-model="val[2]" language="regex" placeholder="Regex with capture groups (?P<field_name>.*?)" />-->
          </CInputGroup>
          <CInputGroup v-else-if="val[0] == 'REGEX_SUB'">
            <CFormTextarea class="" id="regex" v-model="val[3]" placeholder="Regex pattern to search for replacement" />
            <!--<CodeTextarea class="form-control" v-model="val[3]" language="regex" placeholder="Regex pattern to search for replacement" />-->
            <CFormTextarea class="" id="sub" v-model="val[4]" placeholder="Substitute" />
          </CInputGroup>
        </CCol>
      </CRow>
    </CForm>
    <CCol xs="auto">
      <CButton @click="append" @click.stop.prevent color="secondary" v-c-tooltip="'Add'"><i class="la la-plus la-lg"></i></CButton>
    </CCol>
  </div>

</template>

<script>
import Base from './Base.vue'
import SFormInput from '@/components/SFormInput.vue'

export default {
  extends: Base,
  name: 'Modification',
  emits: ['update:modelValue'],
  components: {
    SFormInput,
  },
  props: {
    modelValue: {type: Array, default: () => []},
    options: {},
  },
  data () {
    return {
      dataValue: this.modelValue,
      documentation: {
        'SET': "Set a field to a given value (string)",
        'DELETE': "Delete a field value",
        'ARRAY_APPEND': "Append a string to an array",
        'ARRAY_DELETE': "Delete an element from an array by value",
        'REGEX_PARSE': "Given a regex with named capture groups, the value of the capture groups will be merged to the record by name",
        'REGEX_SUB': "Search the elements matching a regex, and replace them with a substitute",
        'KV_SET': "Map a field to a value in a key-value dictionary",
      },
      operations: [
        {value: 'SET', text: 'Set'},
        {value: 'DELETE', text: 'Delete'},
        {value: 'ARRAY_APPEND', text: 'Append (to array)'},
        {value: 'ARRAY_DELETE', text: 'Delete (from array)'},
        {value: 'REGEX_PARSE', text: 'Regex parse (capture)'},
        {value: 'REGEX_SUB', text: 'Regex sub'},
        {value: 'KV_SET', text: 'Key-value mapping'},
      ],
    }
  },
  methods: {
    append () {
      this.dataValue.push(['SET', '', ''])
    },
    remove (index) {
      this.dataValue.splice(index, 1)
    },
  },
  watch: {
    dataValue: {
      handler: function () {
        this.$emit('update:modelValue', this.dataValue)
      },
      immediate: true
    },
  },
}
</script>

<style scoped lang="scss">

.input-group {
  .form-select, .form-control, btn {
    margin: -1px;
  }

}

</style>
