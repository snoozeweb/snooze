<template>

    <CForm @submit.prevent>

      <!-- No condition case (always true) -->
      <template v-if="dataValue.type == 'alwaysTrue'">
        <CBadge style="font-size: 0.875rem;" color="danger" class="col me-2 align-middle">Always true</CBadge>
        <CButton
          class="col"
          @click="dataValue = defaultCondition()"
          color="secondary">
          <i class="la la-plus la-lg"></i>
        </CButton>
      </template>

      <!-- AND/OR/NOT case -->
      <template v-if="['logic', 'not'].includes(dataValue.type)">
        <CForm inline class="row g-0">
        <CCol xs="auto">
        <CInputGroup>
          <CFormSelect v-model="operation" :value="dataValue.operation" class="w-auto">
            <option v-for="option in LOGIC_OPTIONS" :value="option.value">{{ option.text }}</option>
          </CFormSelect>
          <CButton @click="logicAdd" @click.stop.prevent color="secondary" v-if="dataValue.operation != 'NOT'">
            <i class="la la-plus la-lg"></i>
          </CButton>
          <CButton @click="escalateDelete" @click.stop.prevent color="danger"><i class="la la-trash la-lg"></i></CButton>
        </CInputGroup>
        </CCol>
        </CForm>
        <ul class="ps-3">
          <CForm @submit.prevent class="pt-1" v-for="(arg, i) in dataValue.args" v-bind:key="arg.id">
            <ConditionChild v-model="dataValue.args[i]" :index="i" @delete-event="deleteCondition" />
          </CForm>
        </ul>
      </template>

      <!-- `field=value` case (and other operations) -->
      <template v-else-if="['binary', 'unary'].includes(dataValue.type)">
        <CInputGroup>
          <CFormInput v-model="dataValue.args[0]" placeholder="Field" style="flex: 0 0 auto; width: 25%"/>
          <CFormSelect v-model="operation" :value="dataValue.operation" style="flex: 0 0 auto; width: 15%">
            <option v-for="option in OPERATION_OPTIONS" :value="option.value">{{ option.text }}</option>
          </CFormSelect>
          <SFormInput v-model="dataValue.args[1]" placeholder="Value" v-if="dataValue.type == 'binary'"/>
          <CButton @click="fork" @click.stop.prevent color="secondary"><i class="la la-plus la-lg"></i></CButton>
          <CButton @click="dataValue = defaultCondition()" @click.stop.prevent color="info"><i class="la la-redo-alt la-lg"></i></CButton>
          <CButton @click="escalateDelete" @click.stop.prevent color="danger"><i class="la la-trash la-lg"></i></CButton>
        </CInputGroup>
      </template>

    </CForm>
  <!-- End {{ dataValue }} -->
</template>

<script>

// Options for the operation selector
const OPERATION_OPTIONS = [
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
]

// Options for the logic selector
const LOGIC_OPTIONS = [
  {value: 'OR', text: 'OR'},
  {value: 'AND', text: 'AND'},
  {value: 'NOT', text: 'NOT'},
]

import { ConditionObject, OPERATION_TYPE } from '@/utils/condition'
import SFormInput from '@/components/SFormInput.vue'

export default {
  name: 'ConditionChild',
  emits: ['update:modelValue', 'deleteEvent'],
  components: {
    SFormInput,
  },
  props: [
    "modelValue",
    "index",
    "root",
  ],
  created () {
    this.OPERATION_TYPE = OPERATION_TYPE
    this.OPERATION_OPTIONS = OPERATION_OPTIONS
    this.LOGIC_OPTIONS = LOGIC_OPTIONS
  },
  data () {
    return {
      dataValue: this.modelValue,
    }
  },
  methods: {
    // Return a new condition object
    defaultCondition () {
      return new ConditionObject('=', ['', ''])
    },
    // Triggered when we push the `+` button for a logic operator (AND/OR)
    logicAdd() {
      this.dataValue.args.push(this.defaultCondition())
    },
    // Delete a condition at the given index in a AND/OR condition
    deleteCondition(index) {
      this.dataValue.args.splice(index, 1)
      // Correct the condition if it's invalid (AND/OR less than 2 argument,
      // NOT with less than 1 argument)
      if (this.dataValue.type == 'logic' && this.dataValue.args.length < 2) {
        this.dataValue = this.dataValue.args[0]
      }
      if (this.dataValue.type == 'not' && this.dataValue.args.length < 1) {
        this.dataValue = new ConditionObject('')
      }
    },
    // Triggered when the delete button is pressed for any condition. This will
    // escalate the delete operation to the parent condition, or reset the condition
    // if it's the root condition.
    escalateDelete() {
      if (this.root) {
        this.dataValue = new ConditionObject('')
      } else {
        console.log(`escalateDelete: index=${this.index}`)
        this.$emit('deleteEvent', this.index)
      }
    },
    // Trigerred when pushing the `+` button for a normal condition (a=x)
    // This will create a logic operator at the place of the condition, resulting
    // in "a=1 AND defaultCondition()"
    fork () {
      this.dataValue = this.dataValue.combine('AND', this.defaultCondition())
    },
  },
  computed: {
    // We compute the operation for logic selectors in order to correct the number
    // of arguments that is different for OR/AND and NOT.
    operation: {
      get: function() { this.dataValue.operation },
      set: function(op) {
        if (op == 'NOT') {
          this.dataValue.args = [this.dataValue.args[0]]
        } else if ((op == 'AND' || op == 'OR') && this.dataValue.args.length < 2) {
          this.logicAdd()
        }
        this.dataValue.operation = op
      },
    },
  },
  watch: {
    dataValue: {
      handler: function () {
        this.$emit('update:modelValue', this.dataValue)
      },
    }
  },
}
</script>
