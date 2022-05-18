<template>
  <div>
    <CCard>
      <CCardBody class="p-3">
        <CRow>
          <CCol xs="3">
            <h4 id="traffic" class="card-title mb-0">Alerts</h4>
            <div class="small text-muted">Dashboard</div>
          </CCol>
          <CCol xs="9">
            <CButton class="float-end" @click="refresh()" color="primary" v-c-tooltip="{content: 'Reload'}"><i class="la la-refresh la-lg"></i></CButton>
            <DateTime class="float-end me-3 d-inline-block" style="width: 380px" v-model="datetime" ref='datetimepicker'/>
            <CButton class="float-end me-3" size="sm" @click="refresh(1,'d')" color="info" v-c-tooltip="{content: '1 day'}">Daily</CButton>
            <CButton class="float-end me-2" size="sm" @click="refresh(1,'w')" color="info" v-c-tooltip="{content: '1 week'}">Weekly</CButton>
            <CButton class="float-end me-2" size="sm" @click="refresh(1,'M')" color="info" v-c-tooltip="{content: '1 month'}">Monthly</CButton>
            <CButton class="float-end me-2" size="sm" @click="refresh(1,'y')" color="info" v-c-tooltip="{content: '1 year'}">Yearly</CButton>
          </CCol>
        </CRow>
        <ChartMain style="height:350px;margin-top:10px;"
          ref="mainchart"
          :datasets="datasets"
          @click="main_click"
        />
      </CCardBody>
      <CCardFooter class="p-2">
        <CRow class="text-center">
          <CCol class="mb-sm-2 mb-0" v-for="(source, key) in datasource" :key="source.label">
            <div><CBadge class="pointer" color="secondary" :style="source.hidden ? '' : gen_color(source.color)" @click="toggle(key)">{{ source.label }}</CBadge></div>
            <strong>{{ pp_number(data[key]) }} ({{ Math.round(100*(data[key] || 0)/(data[data_ref] || 1)) }}%)</strong>
            <div class="progress-xs mt-2 progress">
              <div class="progress-bar progress-bar-striped progress-bar-animated"
                role="progressbar" aria-valuemin="0" aria-valuemax="100"
                :style="'width: '+Math.round(100*data[key]/(data[data_ref] || 1))+'%; background-color:'+source.color"
                :aria-valuenow="Math.round(100*data[key]/(data[data_ref] || 1))"
              />
            </div>
          </CCol>
        </CRow>
      </CCardFooter>
    </CCard>
    <CRow class="mt-3">
      <CCol md="3" sm="12">
        <CCard>
          <CCardHeader class="py-2 px-3">Alerts by Source</CCardHeader>
          <CCardBody class="p-2">
            <ChartDoughnut style="min-height:300px"
              :datasets="this.data.split_data['alert_hit__source__']"
            />
          </CCardBody>
        </CCard>
      </CCol>
      <CCol md="3" sm="12">
        <CCard>
          <CCardHeader class="py-2 px-3">Alerts by Environment</CCardHeader>
          <CCardBody class="p-2">
            <ChartDoughnut style="min-height:300px"
              :datasets="this.data.split_data['alert_hit__environment__']"
            />
          </CCardBody>
        </CCard>
      </CCol>
      <CCol md="6" sm="12">
        <CCard>
          <CCardHeader class="py-2 px-3">Actions</CCardHeader>
          <CCardBody class="p-2">
            <ChartBar style="min-height:300px"
              sort
              :datasets="[
                {label: 'Action success', color: hexToRgba(theme_colors.success, 50), bordercolor: theme_colors.success, data: this.data.split_data['action_success__name__'] || {}},
                {label: 'Action error', color: hexToRgba(theme_colors.danger, 50), bordercolor: theme_colors.danger, data: this.data.split_data['action_error__name__'] || {}},
              ]"
            />
          </CCardBody>
        </CCard>
      </CCol>
    </CRow>
    <CRow class="mt-3">
      <CCol md="6" sm="12">
        <CCard>
          <CCardHeader class="py-2 px-3">Throttled Alerts</CCardHeader>
          <CCardBody class="p-2">
            <ChartBar style="min-height:300px"
              sort
              :datasets="[
                {color: hexToRgba(theme_colors.tertiary, 50), bordercolor: theme_colors.tertiary, data: this.data.split_data['alert_throttled__name__'] || {}},
              ]"
            />
          </CCardBody>
        </CCard>
      </CCol>
      <CCol md="6" sm="12">
        <CCard>
          <CCardHeader class="py-2 px-3">Snoozed Alerts</CCardHeader>
          <CCardBody class="p-2">
            <ChartBar style="min-height:300px"
              sort
              :datasets="[
                {color: hexToRgba(theme_colors.warning, 50), bordercolor: theme_colors.warning, data: this.data.split_data['alert_snoozed__name__'] || {}},
              ]"
            />
          </CCardBody>
        </CCard>
      </CCol>
    </CRow>
    <CRow class="mt-3">
      <CCol md="12" sm="12">
        <CCard>
          <CCardHeader class="py-2 px-3">Alert by Weekday</CCardHeader>
          <CCardBody class="p-2">
            <ChartBar style="min-height:300px"
              :datasets="datasets_weekday"
            />
          </CCardBody>
        </CCard>
      </CCol>
    </CRow>
    <CRow class="mt-3">
      <CCol md="12">
        <CCard>
          <CCardHeader class="py-2 px-3">
            Last 10 Comments
          </CCardHeader>
          <CCardBody class="p-2">
            <SDataTable
              ref="table"
              class="mb-0"
              :items="tableItems"
              :fields="tableFields"
              size="sm"
              no-sorting
              striped
              outlined
            >
              <template v-slot:type="row">
                <div :class="'bg-' + get_alert_color(row.item['type']) + ' me-1 text-white rounded p-2'" v-c-tooltip="get_alert_tooltip(row.item['type'])">
                  <i :class="'la ' + get_alert_icon(row.item['type']) + ' la-2x'"></i>
                </div>
              </template>
              <template v-slot:user="row">
                <strong>{{ row.item.user.name }}</strong>
                <div class="small text-muted">
                  Last login: <strong>{{ row.item.user.last_login }}</strong>
                </div>
              </template>
              <template v-slot:message="row">
                <div>{{ row.item.message }}</div>
              </template>
              <template v-slot:date="row">
                {{row.item.date}}
              </template>
              <template v-slot:host="row">
                {{row.item.host}}
              </template>
              <template v-slot:alert="row">
                {{row.item.alert}}
              </template>
              <template v-slot:button="row">
                <CButton size="sm" color="secondary" @click="$router.push(get_link(row.item.record_uid))" v-c-tooltip="{content: 'Search'}"><i class="la la-link la-lg"></i></CButton>
              </template>
            </SDataTable>
          </CCardBody>
        </CCard>
      </CCol>
    </CRow>
  </div>
</template>

<script>
import ChartMain from '@/components/ChartMain.vue'
import ChartDoughnut from '@/components/ChartDoughnut.vue'
import ChartBar from '@/components/ChartBar.vue'
import SDataTable from '@/components/SDataTable.vue'
import DateTime from '@/components/form/DateTime.vue'
import moment from 'moment'
import { get_data, pp_number, trimDate, get_alert_icon, get_alert_color, get_alert_tooltip, truncate_message } from '@/utils/api'
import { hexToRgba, theme_colors, gen_color } from '@/utils/colors'
import { API } from '@/api'

const weekdays = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']
const format = "YYYY-MM-DDTHH:mmZ"

export default {
  name: 'Dashboard',
  components: {
    ChartMain,
    ChartDoughnut,
    ChartBar,
    SDataTable,
    DateTime,
  },
  data () {
    return {
      items: [],
      items_weekday: [],
      selected: 'Month',
      date_from: {},
      date_until: {},
      datetime: {},
      last_datetime: {},
      groupby: 24,
      labels: [],
      datasets: [],
      datasets_weekday: [],
      data: {values: {}, split_data: {}},
      loaded: false,
      get_data: get_data,
      gen_color: gen_color,
      get_alert_icon: get_alert_icon,
      get_alert_color: get_alert_color,
      get_alert_tooltip: get_alert_tooltip,
      hexToRgba: hexToRgba,
      theme_colors: theme_colors,
      pp_number: pp_number,
      data_ref: 'alert_hit__source',
      split_data: ['alert_hit__source__', 'alert_hit__environment__', 'notification_sent__name__', 'action_success__name__', 'action_error__name__', 'alert_throttled__name__', 'alert_snoozed__name__'],
      datasource: {
        'alert_hit__source': {label: 'Alerts', color: theme_colors.info},
        'alert_throttled__name': {label: 'Throttled', color: theme_colors.tertiary},
        'alert_snoozed__name': {label: 'Snoozed', color: theme_colors.warning},
        'notification_sent__name': {label: 'Notification sent', color: theme_colors.success},
        'action_error__name': {label: 'Action error', color: theme_colors.danger}
      },
      tableItems: [],
      tableFields: [
        { key: 'type', label: '', tdClass: 'text-center align-middle', thStyle: {width: '1%'}},
        { key: 'user', tdClass: 'align-middle'},
        { key: 'date', tdClass: 'align-middle'},
        { key: 'message', tdClass: 'align-middle text-break'},
        { key: 'host', tdClass: 'align-middle singleline'},
        { key: 'alert', tdClass: 'align-middle text-break'},
        { key: 'button', label: '', tdClass: 'align-middle pe-2', thStyle: {width: '1%'}},
      ]
    }
  },
  mounted () {
    this.reload()
  },
  methods: {
    reload() {
      this.datetime = {from: this.$route.query.from || moment().add(-1, 'd').format(format), until: this.$route.query.until || moment().format(format)}
      this.last_datetime = Object.assign({}, this.datetime)
      this.reload_charts()
      this.get_comments(10)
    },
    refresh(dur, unit) {
      if (dur && unit) {
        this.datetime = {from: moment().add(-dur, unit).format(format), until: moment().format(format)}
      }
      console.log(this.datetime)
      console.log(this.last_datetime)
      if (this.last_datetime.from != this.datetime.from || this.last_datetime.until != this.datetime.until) {
        this.$router.push({ path: this.$router.currentRoute.value.path, query: {from: this.datetime.from, until: this.datetime.until} })
        this.last_datetime = Object.assign({}, this.datetime)
        console.log(this.last_datetime)
      }
    },
    reload_charts() {
      if (this.$refs.datetimepicker) {
        this.$refs.datetimepicker.datavalue = [this.datetime.from, this.datetime.until]
      }
      this.date_from = moment(this.datetime.from)
      this.date_until = moment(this.datetime.until)
      var duration = moment.duration(this.date_until.diff(this.date_from))
      var hours = duration.as('hours')
      var days = duration.as('days')
      var months = duration.as('months')
      var years = duration.as('years')
      if (days < 2) {
        this.groupby = 'hour'
      } else if (months < 2) {
        this.groupby = 'day'
      } else if (years < 2) {
        this.groupby = 'month'
      } else {
        this.groupby = 'year'
      }
      this.get_data('stats', {}, {'date_from': this.date_from.unix(), 'date_until': this.date_until.unix(), 'groupby': this.groupby}, this.update_data)
      this.get_data('stats', {}, {'date_from': this.date_from.unix(), 'date_until': this.date_until.unix(), 'groupby': 'weekday'}, this.update_weekday)
    },
    update_data(response) {
      if (response.data) {
      	this.items = response.data.data
        this.update_datasets()
      }
    },
    update_datasets() {
      this.data = {values: {}, split_data: {}}
      var current = 0
      var sources = Object.keys(this.datasource)
      var date
      var match
      var date_current = this.date_from.startOf(this.groupby)
      var date_end = this.date_until.add(1, this.groupby).startOf(this.groupby).unix()
      var items_len = this.items.length
      var target_date
      if (this.items[current]) {
        target_date = moment(this.items[current]['_id']).startOf(this.groupby)
      }
      this.split_data.forEach(f => this.data.split_data[f] = {})
      sources.forEach(f => {
        this.data.values[f] = []
        this.data[f] = 0
      })
      while (date_current.unix() < date_end) {
        date = date_current.unix()*1000
        this.data[date] = {}
        sources.forEach(f => this.data.values[f].push({'x': date, 'y': 0}))
        if (this.items[current] && target_date.unix() == date_current.unix()) {
          this.items[current].data.forEach(metric => {
            this.split_data.forEach(f => {
              if (metric.key.startsWith(f)) {
                var label = metric.key.substring(f.length)
                this.data.split_data[f][label] = (this.data.split_data[f][label] || 0) + metric.value
              }
            })
            match = sources.filter(f => metric.key.startsWith(f))
            if (match.length > 0) {
              if (this.data[date][match[0]] == undefined) {
                this.data[date][match[0]] = 0
              }
              this.data[date][match[0]] += metric.value
              this.data[match[0]] += metric.value
            }
          })
          Object.keys(this.data[date]).forEach(metric =>
            this.data.values[metric][this.data.values[metric].length - 1].y = this.data[date][metric]
          )
          current += 1
          if (current < items_len) {
            target_date = moment(this.items[current]['_id']).startOf(this.groupby)
          }
        }
        date_current.add(1, this.groupby)
      }
      this.datasets = []
      sources.forEach(f =>
        this.datasets.push(
          {
            label: this.datasource[f].label,
            backgroundColor: hexToRgba(this.datasource[f].color, 20),
            borderColor: this.datasource[f].color,
            pointHoverBackgroundColor: this.datasource[f].color,
            borderWidth: 2,
            data: this.data.values[f],
            hidden: this.datasource[f].hidden,
            tension: 0.3,
            fill: true,
          }
        )
      )
    },
    update_weekday(response) {
      if (response.data) {
        var match
        var date_until = moment(this.datetime.until)
        var sources = Object.keys(this.datasource)
        var weekday_metrics = {}
        this.datasets_weekday = []
      	this.items_weekday = response.data.data
        this.items_weekday.forEach(d => {
          d.data.forEach(metric => {
            match = sources.filter(f => metric.key.startsWith(f))
            if (match.length > 0) {
              if (weekday_metrics[match[0]] == undefined) {
                weekday_metrics[match[0]] = {}
                for (var i = 0; i < 7; i++) {
                  weekday_metrics[match[0]][weekdays[(i+date_until.weekday())%7]] = 0
                }
              }
              weekday_metrics[match[0]][weekdays[d._id-1]] += metric.value
            }
          })
        })
        Object.keys(weekday_metrics).forEach(metric => {
          this.datasets_weekday.push({label: this.datasource[metric].label, color: hexToRgba(this.datasource[metric].color, 50), bordercolor: this.datasource[metric].color, data: weekday_metrics[metric]})
        })
      }
      this.loaded = true
    },
    toggle(key) {
      if (this.datasource[key].hidden) {
        this.datasource[key].hidden = false
        this.datasets[Object.keys(this.datasource).indexOf(key)].hidden = false
      } else {
        this.datasource[key].hidden = true
        this.datasets[Object.keys(this.datasource).indexOf(key)].hidden = true
      }
      this.$refs.mainchart.$refs.chart.updateChart();
    },
    get_comments(n) {
      var query = ['EXISTS', 'name']
      var options = {
        perpage: n,
        pagenb: 1,
        orderby: 'date',
        asc: false,
      }
      this.get_data('comment', query, options, this.comments_callback)
    },
    comments_callback(response) {
      var users = []
      var records_uid = []
      if (response.data) {
        this.tableItems = []
        var user = {}
        response.data.data.forEach(comment => {
          user = comment.name + '_@_' + comment.method
          if (users.indexOf(user) == -1) {
            users.push(user)
          }
          if (records_uid.indexOf(comment.record_uid) == -1) {
            records_uid.push(comment.record_uid)
          }
          this.tableItems.push({
            type: comment.type,
            user: { name: comment.name, method: comment.method, last_login: 'Unknown' },
            message: comment.message,
            date: trimDate(comment.date),
            record_uid: comment.record_uid,
            host: '',
            alert: '',
          })
        })
        if (users.length > 0) {
          users = users.map(u => this.query_name(u))
          var query = users.reduce((a, b) => ['OR', a, b])
          this.get_data('user', query, {}, this.users_callback)
          this.get_data('profile/general', query, {}, this.users_callback)
          records_uid = records_uid.map(r => ['=', 'uid', r])
          query = records_uid.reduce((a, b) => ['OR', a, b])
          this.get_data('record', query, {}, this.records_callback)
        }
      }
    },
    query_name(a) {
      var name_method = a.split('_@_')
      return ['AND', ['=', 'name', name_method[0]], ['=', 'method', name_method[1]]]
    },
    users_callback(response) {
      if (response.data) {
        this.tableItems.forEach(row => {
          response.data.data.forEach(u => {
            if (row.user.name == u.name && row.user.method == u.method) {
              if (u.last_login) {
                row.user.last_login = trimDate(u.last_login)
              }
              if (u.display_name) {
                row.user.name =  u.display_name
              }
            }
          })
        })
      }
    },
    records_callback(response) {
      if (response.data) {
        this.tableItems.forEach(row => {
          response.data.data.forEach(r => {
            if (row.record_uid == r.uid) {
              row.host = r.host
              row.alert = truncate_message(r.message)
            }
          })
        })
      }
    },
    get_link(name) {
      var escaped_name = JSON.stringify(name)
      return {
          path: 'record',
          query: {
          tab: 'All',
          s: encodeURIComponent(`uid=${escaped_name}`),
        },
      }
    },
    main_click(point) {
      if (point.y > 0) {
        var date_from = point.x / 1000
        var date_until = moment(point.x).add(1, this.groupby).unix()
        this.$router.push({
            path: 'record',
            query: {
            tab: 'All',
            s: encodeURIComponent(`date_epoch > ${date_from} and date_epoch < ${date_until}`),
          },
        })
      }
    },
  },
  watch: {
    $route() {
      if (this.loaded && this.$route.path == '/dashboard') {
        this.$nextTick(this.reload);
      }
    }
  },
}
</script>
