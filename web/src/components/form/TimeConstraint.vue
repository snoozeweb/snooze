<template>
  <div>
    <h5 v-if="Object.keys(datavalue).length === 0 && datavalue.constructor === Object"><b-badge variant="primary">Forever</b-badge></h5>
    <b-form-group class="m-0">
      <template v-for="(constraint, ctype) in datavalue">
        <b-row v-for="(val, k) in constraint" :key="ctype+'_'+k" class="mb-2">
          <b-col cols="1">
            <b-button variant="danger" size="lg" v-on:click="remove_component(ctype, k)">X</b-button>
          </b-col>
          <b-col cols="11">
            <component
              :is="detect_constraint(ctype)"
              :id="'component_'+ctype+'_'+k"
              v-model="constraint[k]"
            />
          </b-col>
        </b-row>
      </template>
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
    value: {type: Object, default: () => {}},
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
      datavalue: this.value || {},
    }
  },
  methods: {
    add_component(component_type) {
      if (!(component_type in this.datavalue)) {
        this.$set(this.datavalue, component_type, [])
      }
      this.datavalue[component_type].splice(this.datavalue[component_type].length, 1, {})
    },
    remove_component(component_type, component_index) {
      this.datavalue[component_type].splice(component_index, 1)
      if (this.datavalue[component_type].length == 0) {
        this.$delete(this.datavalue, component_type)
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
      handler(v) { this.$emit('input', this.datavalue) },
      deep: true,
      immediate: true
    },
  },
}
</script>
