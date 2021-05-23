<template>
<div>
  <b-form-tags
    input-id="tags-state-event"
    v-model="datavalue"
    placeholder="Enter fields separated by space"
    separator=" "
    @tag-state="onTagState"
    remove-on-delete
  ></b-form-tags>
</div>
</template>

<script>

import Base from './Base.vue'

export default {
  extends: Base,
  name: 'Condition',
  props: {
    value: {type: Array, default: () => []},
    options: {},
  },
  data () {
    return {
      datavalue: this.value,
			validTags: [],
      invalidTags: [],
      duplicateTags: [],
    }
  },
  methods: {
    onTagState(valid, invalid, duplicate) {
      this.validTags = valid
      this.invalidTags = invalid
      this.duplicateTags = duplicate
    },
  },
  watch: {
    datavalue () { this.$emit('input', this.datavalue) }
  },
}
</script>
