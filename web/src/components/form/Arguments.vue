<template>
  <div>
    <b-form inline v-for="(argument, index) in this.arguments" :key="index">
    <b-input-group class="pb-1">
      <b-form-input v-model=argument[0] placeholder="--mykey"  />
      <b-form-input v-model=argument[1] placeholder="my_value" />
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
  name: 'Arguments',
  props: {
    value: {type: Array, default: () => []},
    options: {},
  },
  data () {
    return {
      arguments: this.value,
      add_key: null,
      add_value: null,
    }
  },
  methods: {
    append () {
      this.arguments.push(['', ''])
      //this.add_key = null
      //this.add_value = null
    },
    remove (index) {
      this.arguments.splice(index, 1)
    }
  },
}
</script>
