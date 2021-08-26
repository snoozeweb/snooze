<template>
  <div>
    <b-card header="Mail" header-class='text-center font-weight-bold'>
      <h5>Header</h5>
      <b-table
        :items="infos"
        :fields="fields"
        thead-class="d-none"
        small
      >
      </b-table>
      <br />
      <div v-if="body_plain_data">
        <h5>Body</h5>
        <div class="multiline">
          {{ body_plain_data[0] }}
          <span v-if="more">{{ body_plain_data[1] }}</span>
          <br />
          <a v-if="body_plain_data[1] != ''" @click="more = !more" href="#">
            <span v-if="more">less</span>
            <span v-else>more</span>
          </a>
        </div>
      </div>
      <div v-if="body_html">
        <h5>HTML body</h5>
        <div v-html-safe="body_html" />
      </div>
    </b-card>
  </div>
</template>

<script>

import { more } from '@/utils/api'

export default {
  name: 'Mail',
  props: {
    smtp: {type: Object},
  },
  data () {
    return {
      header: this.smtp.header,
      relays: this.smtp.relays,
      body_plain: this.smtp.body.plain,
      body_html: this.smtp.body.html,
      more: false,
      fields: [
        {key: 'name', tdClass: 'bold'},
        {key: 'value', tdClass: 'border-left'},
      ],
    }
  },
  computed: {
    body_plain_data () {
      if (this.body_plain) {
        return more(this.body_plain)
      } else {
        return false
      }
    },
    infos () {
      return Object.keys(this.header)
        .reduce((obj, key) => {
          obj.push({name: key, value: this.header[key]})
          return obj
        }, [])
    }
  },
}
</script>
