<template>
  <CInputGroup>
    <CFormInput placeholder="Search" type="search" v-model="datavalue"/>
    <CButton block color="primary" type="submit" @click="search">
        <i class="la la-search la-lg"></i>
    </CButton>
    <CButton block color="secondary" type="reset" @click="clear">
        Clear
    </CButton>
  </CInputGroup>
</template>

<script>

export default {
  props: {
    modelValue: {type: String, default: () => ''},
  },
  emits: ['update:modelValue', 'clear', 'search'],
  data () {
    return {
      datavalue: this.modelValue,
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
      this.$emit('update:modelValue', this.datavalue)
    }
  },
}
</script>
