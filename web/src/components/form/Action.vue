<template>

  <div>
    <b-form inline v-for="(val, index) in this.datavalue" :key="index">
    <b-input-group class="pb-1">
      <b-form-select v-model="val[0]" :options="operations" style="width: auto"/>
      <b-form-input v-model="val[1]"/>
      <b-form-input v-model="val[2]"/>
      <b-input-group-append>
        <b-button v-on:click="remove(index)" variant="danger"><i class="la la-trash la-lg"></i></b-button>
      </b-input-group-append>
    </b-input-group>
    </b-form>
    <b-button v-on:click="append()"><i class="la la-plus la-lg"></i></b-button>
  </div>

</template>

<script>

import Base from './Base.vue'

export default {
  extends: Base,
  name: 'Action',
  props: {
    value: {type: Array, default: () => []},
    options: {},
  },
  data () {
    return {
      datavalue: this.value,
      operations: [
				{value: 'SET', text: 'Set'},
        {value: 'DELETE', text: 'Delete'},
        {value: 'ARRAY_APPEND', text: 'Append (to array)'},
        {value: 'ARRAY_DELETE', text: 'Delete (from array)'},
        {value: 'SET_TEMPLATE', text: 'Template'},
      ],
    }
  },
  methods: {
    append () {
      this.datavalue.push(['', '', ''])
      //this.add_key = null
      //this.add_value = null
    },
    remove (index) {
      this.datavalue.splice(index, 1)
    }
  },
  watch: {
    datavalue () { this.$emit('input', this.datavalue) }
  },
}
</script>
