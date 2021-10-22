<template>
  <CChartBar
    :datasets="data.sets"
    :labels="data.labels"
    :options="default_options"
  />
</template>

<script>
import { CChartBar } from '@coreui/vue-chartjs'

export default {
  name: 'ChartBar',
  components: { CChartBar },
  props: {
    datasets: {
      type: Array,
      default: () => [],
    },
    max_items: {
      type: Number,
      default: () => 20,
    },
    sort: {
      type: Boolean,
      default: () => false,
    },
  },
  computed: {
    data () {
      var series = {}
      var series_array = []
      var sets = []
      var counter = 0
      var datasets = Object.keys(this.datasets).sort((a, b) => this.datasets[a].label < this.datasets[b].label ? -1 : 1)
      datasets.forEach(dataset => {
        Object.keys(this.datasets[dataset].data).forEach(k => {
          series[k] = series[k] || []
          for (var i = 0; i < counter - series[k].length; i++) {
            series[k].push(0)
          }
          series[k].push(this.datasets[dataset].data[k])
        })
        counter += 1
      })
      series_array = Object.keys(series).map(key => [key, series[key]])
      if (this.sort) {
        series_array = series_array.sort((a, b) => (a[1][0] < b[1][0]) ? 1: -1)
      }
      series_array = series_array.slice(0, this.max_items)
      datasets.forEach((dataset, i) => {
        sets.push({
          label: this.datasets[dataset].label,
          borderColor: this.datasets[dataset].bordercolor,
          backgroundColor: this.datasets[dataset].color,
          borderWidth: 2,
          data: series_array.map(x => x[1][i] || 0)
        })
      })
      return {sets: sets, labels: series_array.map(x => x[0])}
    },
    default_options () {
      return {
        maintainAspectRatio: false,
        legend: {
          display: this.datasets.length > 1
        },
        scales: {
          yAxes: [{
            ticks: {
              beginAtZero: true,
              precision: 0,
            },
            gridLines: {
              display: true
            }
          }]
        },
      }
    }
  }
}
</script>
