<template>
  <div>
    <h5 v-if="datavalue.length == 0"><b-badge variant="primary">Forever</b-badge></h5>
    <b-form-group class="m-0">
      <b-row v-for="(v, k) in datavalue" :key="k" class="mb-2">
        <b-col cols="1">
          <b-button variant="danger" size="lg" v-on:click="remove_component(k)">X</b-button>
        </b-col>
        <b-col cols="11">
          <component
            :is="v['type']"
            :id="'component_'+v['type']+'_'+k"
            v-model="v['content']"
          />
        </b-col>
      </b-row>
    </b-form-group>
    <b-form inline>
      <b-input-group>
        <b-form-select v-model="selected" :options="components" value="DateTime"/>
        <b-input-group-append>
          <b-button v-on:click="add_component(selected)"><i class="la la-plus la-lg"></i></b-button>
        </b-input-group-append>
      </b-input-group>
    </b-form>
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
  props: {
    value: {type: Array, default: () => []},
  },
  data() {
    return {
      date_toggle: false,
      selected: 'DateTime',
      components: [
        {value: 'DateTime', text: 'DateTime'},
        {value: 'Time', text: 'Time'},
        {value: 'Weekdays', text: 'Weekdays'},
      ],
      datavalue: this.value,
    }
  },
  methods: {
    add_component(component_type) {
      this.datavalue.push({'type': component_type})
    },
    remove_component(component_index) {
      this.datavalue.splice(component_index, 1)
    },
  },
  watch: {
    datavalue: {
      handler(v) { this.$emit('input', this.datavalue) },
      deep: true,
      immediate: true
    },
  },
}
</script>
