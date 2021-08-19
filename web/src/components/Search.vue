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
      this.$emit('search', this.datavalue)
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
