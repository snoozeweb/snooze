<template>
  <div>
    <b-row>
      <b-col>
        <Duration v-model="datavalue.delay" :default_value="default_value.delay" :options="{'custom_label_prefix': 'After ', 'negative_label': 'Immediately'}" />
      </b-col>
      <b-col>
        <Duration v-model="datavalue.every" :default_value="default_value.every" :options="{'custom_label_prefix': 'Send every ', 'negative_label': 'Send'}" />
      </b-col>
      <b-col>
        <Duration v-model="datavalue.total" :default_value="default_value.total" :options="{'custom_label': ' time(s) total', 'negative_label': 'Forever', 'zero_label': 'Nothing'}" />
      </b-col>
    </b-row>
  </div>
</template>

<script>

import Base from './Base.vue'
import Duration from '@/components/form/Duration'

export default {
  extends: Base,
  name: 'Frequency',
  components: {
    Duration,
  },
  props: {
    'value': {type: Object, default: () => {}},
    'options': {type: Array, default: () => []},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
    'placeholder': {type: String, default: () => ''},
    'default_value': {type: Object, default: () => Object.assign({}, {'every': 0, 'total': 1, 'delay': 0})},
  },
  data() {
    return {
      datavalue: [undefined, '', [], {}].includes(this.value) ? (this.default_value == undefined ? {} : this.default_value) : this.value
    }
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('input', this.datavalue)
      },
      immediate: true,
      deep: true,
    },
  },
}

</script>
