<template>
  <div>
    <div v-if="Object.keys(date).length === 0 && date.constructor === Object" class="d-flex align-items-center">
      <b-badge style="font-size: 0.875rem;" variant="primary">Forever</b-badge>
    </div>
    <template v-for="(constraint, ctype) in date">
    <div
      style="white-space:pre"
      v-for="(date_obj, index) in constraint"
      :key="ctype+'_'+index"
    >
      <div v-if="ctype == 'datetime'" class="d-flex align-items-center">
        <b-badge style="font-size: 0.875rem;" variant="info">{{ trimDate(date_obj.from, false) }}</b-badge>
        <i class="las la-arrow-right la-lg"/><b-badge style="font-size: 0.875rem;" variant="primary">{{ trimDate(date_obj.until, false) }}</b-badge>
      </div>
      <div v-else-if="ctype == 'time'" class="d-flex align-items-center">
        <b-badge style="font-size: 0.875rem;" variant="quaternary">{{ trimDate(date_obj.from, false) }}</b-badge>
        <i class="las la-arrow-right la-lg"/><b-badge style="font-size: 0.875rem;" variant="danger">{{ trimDate(date_obj.until, false) }}</b-badge>
      </div>
      <div v-else-if="ctype == 'weekdays'" class="d-flex align-items-center">
        <b-badge style="font-size: 0.875rem;" variant="warning" v-for="(weekday, ind) in date_obj.weekdays" :key="ind" :class="ind != date_obj.weekdays.length - 1 ? 'mr-1' : ''">{{ get_weekday(weekday) }}</b-badge>
      </div>
      <div :class="index != date.length - 1 ? 'm-1' : ''"/>
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
