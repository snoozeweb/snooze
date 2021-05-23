import { API } from '@/api'

export default class SnoozeTable {

  constructor(table_data) {
    this.table_data = table_data
  }

  getQuery(get_query = null, callback = null) {
    var query_list = []
    if (get_query) {
      this.table_data.query = get_query
    }
    for (var q in this.table_data.query) query_list.push(this.table_data.query[q]);
    var complete_query = query_list.reduce((x,y) => (x=='')?y:((y=='')?x:'["AND", '+x+', '+y+']'),'');
    API
      .get("/"+this.table_data.endpoint+(complete_query.length>0?"/?s="+encodeURI(complete_query)+'&':'?')+'perpage='+this.table_data.per_page+'&pagenb='+this.table_data.current_page+'&orderby='+this.table_data.order_by+'&asc='+this.table_data.is_ascending)
      .then(response => {
        this.updateItems(response.data)
        if (callback) {
          callback(response.data)
        }
      })
      .catch(error => console.log(error))
  }

  postQuery(post_data, callback = null) {
    var filtered_post_data = [];
    for (var i = 0; i < post_data.length; i++) {
      filtered_post_data.push(Object.keys(post_data[i]).reduce(function (filtered, key) {
        if (key[0] != '_') {
          filtered[key] = post_data[i][key];
        } else {
          filtered[key] = '';
        }
        return filtered;
      }, {}));
    }
    API
      .post("/"+this.table_data.endpoint, filtered_post_data)
      .then(response => {
        //TODO: handle response.data.length == 0 case
        if (callback) {
          callback(response.data)
        }
      })
      .catch(error => console.log(error))
  }

  deleteQuery(uid, callback = null) {
    API
      .delete('/'+this.table_data.endpoint + '/' + uid)
      .then(response => {
        //TODO: handle response.data.length == 0 case
        if (callback) {
          callback(response.data)
        }
      })
      .catch(error => console.log(error))
  }

  intervalFetchData() {
    setInterval(() => {
      this.getQuery();
    }, 1000);
  }

  containsObject(obj, list) {
    for (var i = 0; i < list.length; i++) {
      if (list[i]["uid"] == obj["uid"]) {
        return true;
      }
    }
    return false;
  }

  updateItems(response) {
    var update = (this.table_data.items.length == 0)?false:true;
    var rows = response['data'];
    this.table_data.nb_rows = response['count'];
    var inserted_rows = 0;
    for (var i = 0; i < rows.length; i++) {
      if (!this.containsObject(rows[i], this.table_data.items)) {
        this.pushRow(rows[i])
        //this.insertRow(0, rows[i]);
        //this.applyRowStyle(response[i]);
        inserted_rows++;
      }
    }
    if(inserted_rows > 0) {
      //this.updateTable(inserted_rows);
    }
  }

  pushRow(row) {
    this.table_data.items.push(row)
  }

  insertRow(ind, row) {
    this.table_data.items.splice(ind, 0, row);
  }

  removeRow(ind, id, evt) {
    let divs = el.querySelectorAll(".data_" + id);
    Velocity(
      divs,
      { opacity: 0 },
      { duration: 500}
    );
    this.table_data.items.splice(ind, 1);
  }

  updateTable(inserted_rows) {
    let divs = document.querySelectorAll(".data_" + this.table_data.items[(this.table_data.current_page-1)*this.table_data.per_page+inserted_rows-1]["uid"]);
    if(divs.length > 0){
      this.updateDivs(inserted_rows)
    } else {
      setTimeout(() => {
      this.updateDivs(inserted_rows)
      }, 0);
    }
  }

  updateDivs(inserted_rows) {
    for (var i = 0; i < inserted_rows; i++) {
      let divs = document.querySelectorAll(".data_" + this.table_data.items[(this.table_data.current_page-1)*this.table_data.per_page+i]["uid"]);
      for (let j = 0; j < divs.length; j++) {
        divs[j].style.opacity = 0;
      }
      Velocity(
        divs,
        { opacity: 1 },
        { duration: 500}
      );
    }
  }

}
