<template>
  <div class="animated fadeIn">
    <CCard no-body ref="main">
      <CCardHeader class="p-2">
        <CNav variant="pills" role="tablist" card>
          <CNavItem>
            <CNavLink active>Status</CNavLink>
          </CNavItem>
          <CNavItem class="ms-auto" link-classes="py-0 pe-0">
            <CButtonToolbar role="group" key-nav>
              <CButton color="secondary" @click="refresh(true)"><i class="la la-refresh la-lg"></i></CButton>
            </CButtonToolbar>
          </CNavItem>
        </CNav>
      </CCardHeader>
      <CCardBody class="p-2" v-if="items.length > 0">
        <CTabContent>
          <CTable
            ref="table"
            :items="items"
            :fields="fields"
            striped
            small
            bordered
          >
            <CTableHead>
              <CTableRow>
                <CTableHeaderCell scope="col" v-for="(field, i) in fields" :key="`${field.key}_${i}`" :class="field.size">
                  {{ capitalizeFirstLetter(field.label || field.key || field) }}
                </CTableHeaderCell>
              </CTableRow>
            </CTableHead>
            <CTableBody>
              <CTableRow v-for="(item, i) in items" :key="i">
                <CTableDataCell scope="row" v-for="(field, k) in fields" :key="`${field.key}_${k}`">
                  <span v-if="field.key == 'healthy'">
                    <Field :data="item[field.key] ? ['OK']: ['ERROR']" colorize/>
                  </span>
                  <span v-else>
                    {{ item[field.key] || '' }}
                  </span>
                </CTableDataCell>
              </CTableRow>
            </CTableBody>
          </CTable>
        </CTabContent>
      </CCardBody>
      <div v-else>
        <span class='m-2'>
          {{ feedback_message }}
        </span>
      </div>
    </CCard>
  </div>
</template>

<script>

import dig from 'object-dig'
import { get_data, capitalizeFirstLetter } from '@/utils/api'
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
      this.get_data('cluster', null, {}, this.callback, feedback)
    },
    callback(response, feedback) {
      this.feedback_message = 'No cluster was configured'
      if (response.data && response.data.data) {
        this.items = response.data.data
        this.items.sort(function(a, b) {
          return a.host - b.host;
        });
        if (feedback) {
          this.$root.show_alert()
        }
      }
    },
  },
  data () {
    return {
      dig: dig,
      get_data: get_data,
      capitalizeFirstLetter: capitalizeFirstLetter,
      feedback_message: '',
      fields: [
        {key: 'host', size: 'w-25'},
        {key: 'port', size: 'w-25'},
        {key: 'version', size: 'w-25'},
        {key: 'healthy', label: 'Status', size: 'w-25'}
      ],
      items: [],
    }
  },
}
</script>
