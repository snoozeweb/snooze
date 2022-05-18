<template>
  <SChart
    type="line"
    :data="{ datasets: datasets, labels: [] }"
    :options="options || defaultOptions"
    :customTooltips="false"
    ref=chart
  />
</template>

<script>
import SChart from '@/components/SChart.vue'

export default {
  name: 'ChartMain',
  emits: ['click'],
  components: {
    SChart
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
  data () {
    return {
      stored_column: undefined,
    }
  },
  computed: {
    defaultOptions () {
      return {
        maintainAspectRatio: false,
        plugins: {
          legend: {
            display: false
          },
          tooltip: {
            intersect: false,
						callbacks: {
							labelColor: function(tooltipItem, chartInstance) {
								return {
									borderColor: tooltipItem.dataset.borderColor,
									backgroundColor: tooltipItem.dataset.borderColor,
									borderWidth: 3,
									borderRadius: 2,
								};
							},
            },
          },
        },
        hover: {
          mode: 'index',
          intersect: false,
          axis: 'x',
        },
        onHover: (e, item) => {
          if (item.length) {
            var point = item[0].element.$context.parsed
            if (point.y == 0) {
              this.stored_column = undefined
              e.chart.canvas.style.cursor = 'default'
            }
            else if (this.stored_column == undefined || (point.y > 0 && point.x != this.stored_column.x)) {
              this.stored_column = point
              e.chart.canvas.style.cursor = 'pointer'
            }
          } else {
            this.stored_column = undefined
            e.chart.canvas.style.cursor = 'default'
          }
        },
        onClick: () => {
          if (this.stored_column) {
            this.$emit('click', this.stored_column)
          }
        },
        scales: {
          x: {
            type: 'timeseries',
            grid: {
              drawOnChartArea: false
            },
            ticks: {
              source: 'data',
              major: {
                enabled: true
              },
              font: function(context) {
                if (context.tick && context.tick.major) {
                  return {
                    weight: 'bold',
                  };
                }
              }
            },
          },
          y: {
            beginAtZero: true,
            ticks: {
              precision: 0,
            },
            grid: {
              display: true
            }
          }
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
