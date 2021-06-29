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
    value: {
      type: Array,
    },
    options: {
      type: Array,
    },
    default_value: {
      type: Array,
    },
  },
  data () {
    return {
      datavalue: [undefined, '', [], {}].includes(this.value) ? (this.default_value == undefined ? [] : this.default_value) : this.value,
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
    datavalue: {
      handler: function () {
        this.$emit('input', this.datavalue)
      },
      immediate: true
    },
  },
}
</script>
