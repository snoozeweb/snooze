<template>
  <div
    style="white-space:pre"
  >{{ trimDate(date, true) }}</div>
</template>

<script>
import moment from 'moment'

export default {
  props: {
    date: {type: String},
  },
  methods: {
    trimDate(date, splitH) {
      if (!date) {
        return 'Empty'
      }
      var mDate = moment(date)
      var newDate = ''
      var now = moment()
      if (mDate.diff(now) < 0) {
        newDate = mDate.format('L HH:mm:ss')
        splitH = false
      } else if (mDate.year() == now.year()) {
        if (mDate.format('MM-DD') == now.format('MM-DD')) {
          if(splitH) {
            newDate = 'Today' + '\n' + mDate.format('HH:mm')
          } else {
            newDate = 'Today' + ' ' + mDate.format('HH:mm')
          }
        } else {
          newDate = mDate.format('MMM Do HH:mm')
        }
      } else {
        newDate = mDate.format('MMM Do YYYY')
      }
      if(splitH) {
        var splitDate = newDate.split(' ')
        if (splitDate.length > 2) {
          newDate = splitDate[0] + ' ' + splitDate[1] + '\n' + splitDate.slice(2).join(' ')
        }
      }
      return newDate
    },
  },
}
</script>
