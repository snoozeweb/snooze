<template>
  <CChartLine
    :datasets="datasets"
    :options="options || defaultOptions"
    :labels="[]"
    ref=chart
  />
</template>

<script>
import { CChartLine } from '@coreui/vue-chartjs'

export default {
  name: 'ChartMain',
  components: {
    CChartLine
  },
  props: {
    datasets: {
      type: Array,
    },
    options: {
      type: Object,
    },
    labels: {
      type: Array,
    },
  },
  mounted () {
    this.$refs.chart.customTooltips.tooltips.intersect = false
    this.$refs.chart.customTooltips.tooltips.axis = 'x'
  },
  computed: {
    defaultOptions () {
      return {
        maintainAspectRatio: false,
        legend: {
          display: false
        },
        hover: {
          mode: 'index',
          intersect: false,
          axis: 'x',
          animationDuration: 0,
        },
        scales: {
          xAxes: [{
            type: 'time',
            distribution: 'series',
            gridLines: {
              drawOnChartArea: false
            },
            ticks: {
              precision: 0,
            },
          }],
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
        elements: {
          point: {
            radius: 0,
            hitRadius: 10,
            hoverRadius: 4,
            hoverBorderWidth: 3
          }
        }
      }
    }
  }
}
</script>
