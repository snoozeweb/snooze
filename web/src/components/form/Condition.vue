<template>

<span>
  <span v-if="datavalue[0] == 'OR' || datavalue[0] == 'AND'">
    <b-form inline>
      <b-input-group>
        <b-form-select v-model="datavalue[0]" :options="logic"/>
        <b-input-group-append>
          <b-button v-on:click="datavalue = datavalue[1]" variant="danger"><i class="la la-trash la-lg"></i></b-button>
        </b-input-group-append>
      </b-input-group>
    </b-form>
    <ul>
      <b-form inline class="pt-1">
        <Condition v-model="datavalue[1]" :parent_value="datavalue[2]">
          <template #parent_comp>
            <b-button v-if="is_not_operation(datavalue[1])" v-on:click="datavalue = datavalue[2]" variant="danger"><i class="la la-trash la-lg"></i></b-button>
          </template>
        </Condition>
      </b-form>
      <b-form inline class="pt-1">
        <Condition v-model="datavalue[2]" :parent_value="datavalue[1]">
          <template #parent_comp>
            <b-button v-if="is_not_operation(datavalue[1])" v-on:click="datavalue = datavalue[2]" variant="danger"><i class="la la-trash la-lg"></i></b-button>
          </template>
        </Condition>
      </b-form>
    </ul>
  </span>
  <span v-else-if="datavalue[0] == 'NOT'">
    <b-form inline>
      <b-input-group>
        <b-form-select v-model="datavalue[0]" :options="logic"/>
        <b-input-group-append>
          <b-button v-on:click="datavalue = datavalue[1]" variant="danger"><i class="la la-trash la-lg"></i></b-button>
        </b-input-group-append>
      </b-input-group>
    </b-form>
    <ul>
      <b-form inline class="pt-1">
        <Condition v-model="datavalue[1]" :parent_value="datavalue[1]">
          <template #parent_comp>
            <b-button v-if="is_not_operation(datavalue[1])" v-on:click="datavalue = datavalue[1]" variant="danger"><i class="la la-trash la-lg"></i></b-button>
          </template>
        </Condition>
      </b-form>
    </ul>
  </span>
  <span v-else>
    <b-form inline>
      <b-input-group>
        <b-form-input v-model="datavalue[1]"/>
        <b-form-select v-model="datavalue[0]" :options="operations" value="="/>
        <b-form-input v-model="datavalue[2]" v-if="datavalue[0] != 'EXISTS'"/>
        <b-input-group-append>
          <b-button v-on:click="datavalue = ['OR', datavalue, []]"><i class="la la-plus la-lg"></i></b-button>
          <slot name="parent_comp"/>
        </b-input-group-append>
      </b-input-group>
    </b-form>
  </span>
</span>

</template>

<script>

import Base from './Base.vue'

export default {
  extends: Base,
  name: 'Condition',
  props: {
    value: {type: Array, default: () => []},
    parent_value: {type: Array, default: () => []},
    options: {},
  },
  data () {
    return {
      datavalue: this.value,
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
        this.$emit('input', this.datavalue)
      },
      immediate: true
    },
  },
}
</script>
