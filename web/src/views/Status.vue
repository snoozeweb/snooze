<template>
  <div class="animated fadeIn">
    <b-card no-body ref="main">
      <b-card-header header-tag="nav" class="p-2">
        <b-nav card-header pills class='m-0'>
          <b-nav-item active>
            Cluster
          </b-nav-item>
          <b-nav-item class="ml-auto" link-classes="py-0 pr-0">
            <b-button-toolbar key-nav>
              <b-button @click="refresh(true)"><i class="la la-refresh la-lg"></i></b-button>
            </b-button-toolbar>
          </b-nav-item>
        </b-nav>
      </b-card-header>
      <b-card-body class="p-2" v-if="items.length > 0">
        <b-table
          ref="table"
          :items="items"
          :fields="fields"
          striped
          small
          bordered
        >
          <template v-slot:cell(healthy)="row">
            <Field :data="dig(row.item, 'healthy') ? ['OK']: ['ERROR']" colorize/>
          </template>
        </b-table>
      </b-card-body>
      <div v-else>
        <span class='m-2'>
          No cluster was configured
        </span>
      </div>
    </b-card>
  <b-alert
    :show="alert_countdown"
    dismissible
    fade
    class="position-fixed fixed-top m-0 rounded-0 text-center"
    style="z-index: 2000;"
    variant="success"
    @dismiss-count-down="a => this.alert_countdown = a"
  >
    Updated
  </b-alert>
  </div>
</template>

<script>

import dig from 'object-dig'
import { get_data } from '@/utils/api'
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
      this.get_data('cluster', null, {}, this.callback, feedback)
    },
    callback(response, feedback) {
      console.log(response)
      if (response.data) {
        this.items = response.data.data
        this.items.sort(function(a, b) {
          return a.host - b.host;
        });
        if (feedback) {
          this.alert_countdown = 1
        }
      }
    },
  },
  data () {
    return {
      dig: dig,
      get_data: get_data,
      alert_countdown: 0,
      fields: [
        {key: 'host'},
        {key: 'port'},
        {key: 'healthy', label: 'Status'}
      ],
      items: [],
    }
  },
}
</script>
