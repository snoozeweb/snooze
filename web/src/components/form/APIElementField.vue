<template>
  <div>
    <SFormTags
      v-model="dataval"
      :tagsOptions="items"
      :lockedTags="data[this.import_keys[0]]"
      :primary="primary"
      size="lg"
      :colorize="colorize"
      :required="required"
    >
    </SFormTags>
  </div>
</template>

<script>
import { API } from '@/api'
import Base from './Base.vue'
import SFormTags from '@/components/SFormTags.vue'
import { get_color } from '@/utils/colors'

export default {
  extends: Base,
  components: {
    SFormTags,
  },
  emits: ['update:modelValue'],
  props: {
    modelValue: {},
    // Endpoint of the API to query and
    // fetch the objects
    endpoint: {
      type: String,
      required: true,
    },
    primary: {
      type: String,
    },
    data: {
      type: Object,
    },
    import_keys: {
      type: Array,
      default: () => [],
    },
    colorize: {
      type: Boolean,
    },
    required: {
      type: Boolean,
      default: () => false
    },
  },
  data() {
    return {
      get_color: get_color,
      datavalue: this.modelValue || [],
      items: [],
    }
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('update:modelValue', this.datavalue)
      },
      immediate: true
    },
  },
  mounted () {
    this.reload_items()
  },
  computed: {
    dataval: {
      get: function () {
        return this.datavalue.concat(this.data[this.import_keys[0]] || [])
      },
      set: function(newvalue) {
        this.datavalue = newvalue.filter((el) => !(this.data[this.import_keys[0]] || []).includes(el));
      }
    },
  },
  methods: {
    reload_items () {
      console.log(`GET /${this.endpoint}`)
      API
        .get(`/${this.endpoint}`)
        .then(response => {
          this.items = response.data.data
        })
    },
  },
}
</script>
