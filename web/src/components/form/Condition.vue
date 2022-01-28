<template>
  <div>
    <!-- Condition dataValue: {{ dataValue }}, modelValue: {{ modelValue }} -->
    <ConditionChild
      v-model="dataValue"
      :root="true"
    ></ConditionChild>
  </div>
</template>

<script>
import { ConditionObject, OPERATION_TYPE } from '@/utils/condition'
import ConditionChild from '@/components/form/ConditionChild.vue'
import Base from './Base.vue'

export default {
  extends: Base,
  name: 'Condition',
  emits: ['update:modelValue'],
  components: { ConditionChild },
  props: {
    modelValue: {type: Array, default: () => [""]},
  },
  data () {
    return {
      dataValue: ConditionObject.fromArray(this.modelValue),
    }
  },
  watch: {
    dataValue: {
      handler: function () {
        var arrayCondition = this.dataValue.toArray()
        console.log(`Condition.modelValue update to: ${arrayCondition}`)
        this.$emit('update:modelValue', arrayCondition)
      },
      deep: true,
    }
  },
}
</script>
