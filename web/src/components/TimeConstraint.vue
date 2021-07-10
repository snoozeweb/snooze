<template>
  <div>
    <div
      style="white-space:pre"
      v-for="(date_obj, index) in date"
      :key="index"
    >
      <div v-if="date_obj.type == 'DateTime'" class="d-flex align-items-center">
        <b-badge style="font-size: 0.875rem;" variant="info">{{ trimDate(date_obj.content.from, false) }}</b-badge>
        <i class="las la-arrow-right la-lg"/><b-badge style="font-size: 0.875rem;" variant="primary">{{ trimDate(date_obj.content.until, false) }}</b-badge>
      </div>
      <div v-else-if="date_obj.type == 'Time'" class="d-flex align-items-center">
        <b-badge style="font-size: 0.875rem;" variant="quaternary">{{ trimDate(date_obj.content.from, false) }}</b-badge>
        <i class="las la-arrow-right la-lg"/><b-badge style="font-size: 0.875rem;" variant="danger">{{ trimDate(date_obj.content.until, false) }}</b-badge>
      </div>
      <div v-else-if="date_obj.type == 'Weekdays'" class="d-flex align-items-center">
        <b-badge style="font-size: 0.875rem;" variant="warning" v-for="(weekday, ind) in date_obj.content.weekdays" :key="ind" :class="ind != date_obj.content.weekdays.length - 1 ? 'mr-1' : ''">{{ get_weekday(weekday) }}</b-badge>
      </div>
      <div :class="index != date.length - 1 ? 'm-1' : ''"/>
    </div>
  </div>
</template>

<script>
import { trimDate, get_weekday } from '@/utils/api'

export default {
  props: {
    date: {type: Array},
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
