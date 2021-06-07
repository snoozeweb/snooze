<template>
<span>
  <div v-if="data != ''">
    <b-row v-for="row in data" :key="row['uid']">
      <b-col>
        <b-card no-body class='mb-2 p-2'>
          <b-card-body class="d-flex p-0 align-items-center">
            <div :class="'bg-' + get_color(row['type']) + ' mr-3 text-white rounded p-2'" v-b-tooltip.hover :title="get_tooltip(row['type'])">
              <i :class="'la ' + get_icon(row['type']) + ' la-2x'"/>
            </div>
            <div>
              <div>
                <span class="font-weight-bold" style="font-size: 1.0rem;">{{ row['name'] }}</span>
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
    <b-form-textarea
      id="textarea"
      v-model="input_text"
      placeholder="Add a comment"
      rows="1"
      max-rows="8"
    ></b-form-textarea>
    <div class='mt-2'>
      <b-button size="sm" variant='primary' @click="add_comment(input_text, 'comment')">Comment</b-button>
      <b-button size="sm" class='float-right' @click="refresh()"><i class="la la-refresh la-lg"/></b-button>
    </div>
  </div>
</span>
</template>

<script>
import DateTime from '@/components/DateTime.vue'
import moment from 'moment'
import { add_items, update_items } from '@/utils/api'
import { API } from '@/api'

export default {
  name: 'Timeline',
  components: {
    DateTime,
  },
  props: {
    record: {type: Object},
  },
  data() {
    return {
      add_items: add_items,
      update_items: update_items,
      data: {},
      input_text: '',
    }
  },
  mounted () {
    this.refresh()
  },
  methods: {
    refresh () {
      console.log(`GET /comment/['=','record_uid','${this.record.uid}']`)
      API
        .get('/comment/' + encodeURIComponent('["=","record_uid","' + this.record.uid + '"]'))
        .then(response => {
          console.log(response)
          this.data = response.data.data
          this.record['comment_count'] = response.data.count
        })
    },
    add_comment(message, type) {
      var comment = {
        record_uid: this.record['uid'],
        type: type,
        message: message,
        date: moment().format(),
      }
      add_items("comment_self", [comment], this.callback)
    },
    callback(response) {
      this.refresh()
      this.input_text = ''
      this.show_toast(response)
    },
    show_toast(response) {
      var title, message, variant
      if (response.data) {
        title = 'Success!'
        variant = 'success'
        message = 'The operation was successful'
      } else {
        title = 'Error'
        message = 'The operation could not be completed'
        variant = 'danger'
      }
      this.$bvToast.toast(message, {
        title: title,
        variant: variant,
        solid: true,
      })
    },
    get_color(type) {
      switch (type) {
        case 'ack':
          return 'gradient-success'
        case 'esc':
          return 'gradient-warning'
        default:
          return 'gradient-primary'
      }
    },
    get_icon(type) {
      switch (type) {
        case 'ack':
          return 'la-thumbs-up'
        case 'esc':
          return 'la-exclamation'
        default:
          return 'la-comment-dots'
      }
    },
    get_tooltip(type) {
      switch (type) {
        case 'ack':
          return 'Ack'
        case 'esc':
          return 'Re-escalation'
        default:
          return 'Comment'
      }
    },
  },
}
</script>
