<template>
  <div>
    <div v-if="Object.keys(date).length === 0 && date.constructor === Object" class="d-flex align-items-center">
      <CBadge style="font-size: 0.875rem;" color="primary">Forever</CBadge>
    </div>
    <div v-else class="mt-1"/>
    <template v-for="(constraint, ctype) in date">
      <div
        style="white-space:pre"
        v-for="(date_obj, index) in constraint"
        :key="ctype+'_'+index"
      >
        <div v-if="ctype == 'datetime'" class="d-flex align-items-center mb-1">
          <CBadge style="font-size: 0.875rem;" color="info">{{ trimDate(date_obj.from, false) }}</CBadge>
          <i class="la la-arrow-right la-lg"></i><CBadge style="font-size: 0.875rem;" color="primary">{{ trimDate(date_obj.until, false) }}</CBadge>
        </div>
        <div v-else-if="ctype == 'time'" class="d-flex align-items-center mb-1">
          <CBadge style="font-size: 0.875rem;" color="quaternary">{{ trimDate(date_obj.from, false) }}</CBadge>
          <i class="la la-arrow-right la-lg"></i><CBadge style="font-size: 0.875rem;" color="danger">{{ trimDate(date_obj.until, false) }}</CBadge>
        </div>
        <div v-else-if="ctype == 'weekdays'" class="d-flex align-items-center flex-wrap">
          <CBadge style="font-size: 0.875rem;" color="warning" v-for="(weekday, ind) in date_obj.weekdays" :key="ind" :class="ind != date_obj.weekdays.length - 1 ? 'me-1 mb-1' : 'mb-1'">{{ get_weekday(weekday) }}</CBadge>
        </div>
      </div>
    </template>
  </div>
</template>

<script>
import { trimDate, get_weekday } from '@/utils/api'

export default {
  props: {
    date: {
      type: Object,
      default: function () {
        return {}
      }
    },
  },
  data () {
    return {
      trimDate: trimDate,
      get_weekday: get_weekday,
    }
  },
  methods: {
  },
}
</script>
