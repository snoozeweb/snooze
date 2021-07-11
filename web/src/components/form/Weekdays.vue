<template>
  <div class="h-100">
    <b-form-group
      v-slot="{ ariaDescribedby }"
      class="d-flex align-items-center mb-0 h-100"
    >
      <b-form-checkbox-group
        v-model="datavalue['weekdays']"
        :options="options"
        :aria-describedby="ariaDescribedby"
        name="weekdays"
      />
    </b-form-group>
  </div>
</template>

<script>
import Base from './Base.vue'

var default_object = {'weekdays': []}

export default {
  extends: Base,
  props: {
    value: {type: Object, default: () => Object.assign({}, default_object)},
  },
  data() {
    return {
      datavalue: {'weekdays': this.value['weekdays'] || []},
      options: [
        {text: 'Monday',    value: 1},
        {text: 'Tuesday',   value: 2},
        {text: 'Wednesday', value: 3},
        {text: 'Thursday',  value: 4},
        {text: 'Friday',    value: 5},
        {text: 'Saturday',  value: 6},
        {text: 'Sunday',    value: 7},
      ],
    }
  },
  watch: {
    datavalue: {
      handler() { this.$emit('input', this.datavalue) },
      immediate: true,
      deep: true,
    },
  },
}
</script>
