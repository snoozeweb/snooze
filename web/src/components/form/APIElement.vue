<template>
  <div>
    <CFormSelect v-model="selected" :value="selected" :required="required" @change="onChange" :class="{ 'is-invalid': required && !checkField, 'is-valid': required && checkField }">
      <option disabled value="" :selected="selected == ''">Please select an option</option>
      <option v-for="item in this.items" :key="item[primary]" :value="item[primary]">
        {{ item['name'] }}
      </option>
    </CFormSelect>
    <CFormFeedback invalid>
      Field is required
    </CFormFeedback>
    <Form v-if="selection && selection[form]" v-model="subcontent" :metadata="selection[form]" class="pt-2"/>
  </div>
</template>

<script>
import { API } from '@/api'
import Base from './Base.vue'
import Form from '@/components/Form.vue'

export default {
  extends: Base,
  emits: ['update:modelValue'],
  components: {
    Form
  },
  props: {
    modelValue: {
      type: [Object, String],
      default: function () {
        return {'selected': '', 'subcontent': {}}
      }
    },
    // Endpoint of the API to query and
    // fetch the objects
    endpoint: {
      type: String,
      required: true,
    },
    primary: {
      type: String,
      required: true,
    },
    form: {
      type: String,
      default: () => 'form',
    },
    subkey: {
      type: String,
    },
    required: {
      type: Boolean,
      default: () => false
    },
  },
  data() {
    return {
      selected: (this.modelValue['selected'] == undefined) ? this.modelValue : this.modelValue['selected'],
      subcontent: this.modelValue['subcontent'],
      items: [],
    }
  },
  watch: {
    selected: {
      handler: function () {
        if (this.subcontent && Object.keys(this.subcontent).length > 0 && this.subcontent.constructor === Object) {
          this.$emit('update:modelValue', {'selected': this.selected, 'subcontent': this.subcontent})
        } else {
          this.$emit('update:modelValue', this.selected)
        }
      },
      immediate: true
    },
    subcontent: {
      handler: function () {
        if (this.subcontent && Object.keys(this.subcontent).length > 0 && this.subcontent.constructor === Object) {
          this.$emit('update:modelValue', {'selected': this.selected, 'subcontent': this.subcontent})
        } else {
          this.$emit('update:modelValue', this.selected)
        }
      },
      deep: true,
      immediate: true
    },
  },
  mounted () {
    this.reload_items()
  },
  methods: {
    reload_items() {
      API
        .get(`/${this.endpoint}`)
        .then(response => {
          if (response.data) {
            this.items = response.data.data
            if (this.subkey && this.items[this.subkey]) {
              this.items = this.items[this.subkey]
            }
          }
        })
    },
    onChange() {
      this.subcontent = {}
    }
  },
  computed: {
    selection() {
      return this.items.find(opt => opt[this.primary] == this.selected)
    },
    checkField () {
      return this.selected != ''
    }
  },
}
</script>
