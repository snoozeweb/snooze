<template>
<span>
  <div v-if="data != ''">
    <b-row v-for="row in data" :key="row['date']">
      <b-col>
        <b-card no-body class='mb-2 p-2'>
          <b-card-body class="d-flex p-0 align-items-center">
            <div :class="'bg-' + get_color(row['type']) + ' mr-3 text-white rounded p-2'" v-b-tooltip.hover :title="get_tooltip(row['type'])">
              <i :class="'la ' + get_icon(row['type']) + ' la-2x'"/>
            </div>
            <div>
              <div>
                <span class="font-weight-bold" style="font-size: 1.0rem;">{{ row['user']['name'] }}</span>
                <span class="font-italic muted"> @<DateTime :date="row['date']" /></span>
              </div>
              <div class="text-muted">
                {{ row['message'] }}
              </div>
            </div>
          </b-card-body>
        </b-card>
      </b-col>
    </b-row>
  </div>
</span>
</template>

<script>
import DateTime from '@/components/DateTime.vue'

export default {
  name: 'Timeline',
  components: {
    DateTime,
  },
  props: {
    data: {type: [Array, String]},
  },
  methods: {
    get_color(type) {
      switch (type) {
        case 'ack':
          return 'gradient-success'
        case 'reescalated':
          return 'gradient-warning'
        default:
          return 'gradient-primary'
      }
    },
    get_icon(type) {
      switch (type) {
        case 'ack':
          return 'la-thumbs-up'
        case 'reescalated':
          return 'la-exclamation'
        default:
          return 'la-comment-dots'
      }
    },
    get_tooltip(type) {
      switch (type) {
        case 'ack':
          return 'Ack'
        case 'reescalated':
          return 'Re-escalation'
        default:
          return 'Comment'
      }
    },
  },
}
</script>
