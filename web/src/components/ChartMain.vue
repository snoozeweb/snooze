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
