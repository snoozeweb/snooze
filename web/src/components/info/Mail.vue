<template>
  <div>
    <CCard>
      <CCardHeader class='card-header-border text-center' style='font-weight:bold'>
        Mail
      </CCardHeader>
      <CCardBody class="p-0">
        <h5 class="ps-2 pt-1">Header</h5>
        <SDataTable
          :items="infos"
          :fields="fields"
          size="sm"
        >
        </SDataTable>
        <br />
        <div v-if="body_plain_data">
          <h5 class="ps-2 pt-1">Body</h5>
          <div class="multiline p-2">
            {{ body_plain_data[0] }}
            <span v-if="more">{{ body_plain_data[1] }}</span>
            <br />
            <CLink v-if="body_plain_data[1] != ''" @click="more = !more" class="pointer">
              <span v-if="more">less</span>
              <span v-else>more</span>
            </CLink>
          </div>
        </div>
        <div v-if="body_html">
          <h5 class="ps-2 pt-1">HTML body</h5>
          <div v-safe-html="body_html" class="p-2"/>
        </div>
      </CCardBody>
    </CCard>
  </div>
</template>

<script>

import { more } from '@/utils/api'
import SDataTable from '@/components/SDataTable.vue'

export default {
  name: 'Mail',
  components: {
    SDataTable,
  },
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
