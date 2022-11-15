<template>
  <div class="animated fadeIn">

  <CCard>
    <CCardHeader>Plugin sync status</CCardHeader>
      <CCardBody>
      <template v-for="item in items" :key="item.name">
        <CBadge :color="item.color" class="mx-1" v-c-popover="">
          {{ item.name }}: {{ item.ratio }}
        </CBadge>
      </template>
      </CCardBody>
  </CCard>

  </div>
</template>

<script>

import dig from 'object-dig'
import { capitalizeFirstLetter } from '@/utils/api'
import { API } from '@/api'
import Field from '@/components/Field.vue'

export default {
  components: {
    Field,
  },
  mounted () {
    this.refresh()
  },
  methods: {
    refresh(feedback = false) {
      this.feedback_message = 'Getting infos...'
      API.get(`/syncer`)
        .then(response => {
          const plugins = response.data['plugin']
          this.items = Object.keys(plugins).map(key => {
            return {
              name: key,
              ratio: `${plugins[key].synced}/${plugins[key].total}`,
              color: ((plugins[key].synced == plugins[key].total) ? 'success' : 'danger'),
            }
          })
        })
    },
  },
  data () {
    return {
      feedback_message: '',
      items: [],
    }
  },
}
</script>
