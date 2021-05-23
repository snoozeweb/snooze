<template>
  <b-input-group>
    <b-form-input placeholder="Search" type="search" v-model="data"/>
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
  data () {
    return {
      data: '',
    }
  },
  methods: {
    search() {
      if (this.data.length > 1 && this.data[0] == '"' && this.data[this.data.length-1] == '"') {
        this.$emit('search', this.data)
      } else {
        this.$emit('search', Parser.parse(this.data))
      }
    },
    clear() {
      this.data = ""
      // Function to call when the clear button is pushed
      this.$emit('clear')
    },
  },
}
</script>
