<template>
  <b-input-group>
    <b-form-input placeholder="Search" type="search" v-model="datavalue"/>
    <b-input-group-append>
      <b-button block variant="primary" type="submit" @click="search">
        <i class="la la-search la-lg"></i>
      </b-button>
    </b-input-group-append>
    <b-input-group-append>
      <b-button block variant="secondary" type="reset" @click="clear">Clear</b-button>
    </b-input-group-append>
  </b-input-group>
</template>

<script>
const Parser = require('@/utils/parser/index')

export default {
  props: {
    value: {type: String, default: () => ''},
  },
  data () {
    return {
      datavalue: this.value,
    }
  },
  methods: {
    search() {
      if (this.datavalue.length > 1 && this.datavalue[0] == '"' && this.datavalue[this.datavalue.length-1] == '"') {
        this.$emit('search', this.datavalue)
      } else {
        this.$emit('search', Parser.parse(this.datavalue))
      }
    },
    clear() {
      this.datavalue = ""
      // Function to call when the clear button is pushed
      this.$emit('clear')
    },
  },
  watch: {
    datavalue () {
      this.$emit('input', this.datavalue)
    }
  },
}
</script>
